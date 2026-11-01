package kube

import (
	"context"
	"encoding/base64"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PodEnv is what a pod's containers read from the cluster and where they read it from: environment variables (literal,
// from a Secret or ConfigMap, or from the pod itself) and the volumes mounted into them. Secret and ConfigMap
// values are never part of it; they are fetched one key at a time, on request (RefValue).
type PodEnv struct {
	Containers []ContainerEnv `json:"containers"`
	Volumes    []PodVolume    `json:"volumes"`
}

type ContainerEnv struct {
	Name    string        `json:"name"`
	Init    bool          `json:"init"`
	Env     []EnvVar      `json:"env"`
	EnvFrom []EnvFromRef  `json:"envFrom"`
	Mounts  []VolumeMount `json:"mounts"`
}

// EnvVar is one variable. Source is literal, secret, configmap, field or resource; for the last three only Detail is
// set (where the value comes from). Value is set only for literals, which are visible in the pod spec anyway.
type EnvVar struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Value    string `json:"value"`
	Ref      string `json:"ref"` // Secret or ConfigMap name
	Key      string `json:"key"`
	Optional bool   `json:"optional"`
	Detail   string `json:"detail"`
}

type EnvFromRef struct {
	Kind     string `json:"kind"` // secret | configmap
	Name     string `json:"name"`
	Prefix   string `json:"prefix"`
	Optional bool   `json:"optional"`
}

type VolumeMount struct {
	Volume   string `json:"volume"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
	SubPath  string `json:"subPath"`
}

type PodVolume struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // secret | configmap | projected | pvc | emptyDir | hostPath | downwardAPI | other
	Ref  string `json:"ref"`  // Secret / ConfigMap / claim name when there is one
	// Sources lists what a projected volume combines.
	Sources []VolumeSource `json:"sources"`
}

type VolumeSource struct {
	Kind string `json:"kind"` // secret | configmap | serviceAccountToken | downwardAPI
	Name string `json:"name"`
}

// podEnvironment reads the references out of a pod spec. Nothing in it needs the cluster.
func podEnvironment(pod *corev1.Pod) PodEnv {
	out := PodEnv{Containers: []ContainerEnv{}, Volumes: []PodVolume{}}
	add := func(cs []corev1.Container, init bool) {
		for _, c := range cs {
			ce := ContainerEnv{Name: c.Name, Init: init, Env: []EnvVar{}, EnvFrom: []EnvFromRef{}, Mounts: []VolumeMount{}}
			for _, e := range c.Env {
				ce.Env = append(ce.Env, envVar(e))
			}
			for _, f := range c.EnvFrom {
				switch {
				case f.SecretRef != nil:
					ce.EnvFrom = append(ce.EnvFrom, EnvFromRef{Kind: "secret", Name: f.SecretRef.Name, Prefix: f.Prefix, Optional: f.SecretRef.Optional != nil && *f.SecretRef.Optional})
				case f.ConfigMapRef != nil:
					ce.EnvFrom = append(ce.EnvFrom, EnvFromRef{Kind: "configmap", Name: f.ConfigMapRef.Name, Prefix: f.Prefix, Optional: f.ConfigMapRef.Optional != nil && *f.ConfigMapRef.Optional})
				}
			}
			for _, m := range c.VolumeMounts {
				ce.Mounts = append(ce.Mounts, VolumeMount{Volume: m.Name, Path: m.MountPath, ReadOnly: m.ReadOnly, SubPath: m.SubPath})
			}
			out.Containers = append(out.Containers, ce)
		}
	}
	add(pod.Spec.InitContainers, true)
	add(pod.Spec.Containers, false)
	for _, v := range pod.Spec.Volumes {
		out.Volumes = append(out.Volumes, podVolume(v))
	}
	return out
}

func envVar(e corev1.EnvVar) EnvVar {
	v := EnvVar{Name: e.Name, Source: "literal", Value: e.Value}
	if e.ValueFrom == nil {
		return v
	}
	v.Value = ""
	switch f := e.ValueFrom; {
	case f.SecretKeyRef != nil:
		v.Source, v.Ref, v.Key = "secret", f.SecretKeyRef.Name, f.SecretKeyRef.Key
		v.Optional = f.SecretKeyRef.Optional != nil && *f.SecretKeyRef.Optional
	case f.ConfigMapKeyRef != nil:
		v.Source, v.Ref, v.Key = "configmap", f.ConfigMapKeyRef.Name, f.ConfigMapKeyRef.Key
		v.Optional = f.ConfigMapKeyRef.Optional != nil && *f.ConfigMapKeyRef.Optional
	case f.FieldRef != nil:
		v.Source, v.Detail = "field", f.FieldRef.FieldPath
	case f.ResourceFieldRef != nil:
		v.Source, v.Detail = "resource", f.ResourceFieldRef.Resource
	}
	return v
}

func podVolume(v corev1.Volume) PodVolume {
	pv := PodVolume{Name: v.Name, Kind: "other", Sources: []VolumeSource{}}
	switch s := v.VolumeSource; {
	case s.Secret != nil:
		pv.Kind, pv.Ref = "secret", s.Secret.SecretName
	case s.ConfigMap != nil:
		pv.Kind, pv.Ref = "configmap", s.ConfigMap.Name
	case s.PersistentVolumeClaim != nil:
		pv.Kind, pv.Ref = "pvc", s.PersistentVolumeClaim.ClaimName
	case s.EmptyDir != nil:
		pv.Kind = "emptyDir"
	case s.HostPath != nil:
		pv.Kind, pv.Ref = "hostPath", s.HostPath.Path
	case s.DownwardAPI != nil:
		pv.Kind = "downwardAPI"
	case s.Projected != nil:
		pv.Kind = "projected"
		for _, p := range s.Projected.Sources {
			switch {
			case p.Secret != nil:
				pv.Sources = append(pv.Sources, VolumeSource{Kind: "secret", Name: p.Secret.Name})
			case p.ConfigMap != nil:
				pv.Sources = append(pv.Sources, VolumeSource{Kind: "configmap", Name: p.ConfigMap.Name})
			case p.ServiceAccountToken != nil:
				pv.Sources = append(pv.Sources, VolumeSource{Kind: "serviceAccountToken", Name: ""})
			case p.DownwardAPI != nil:
				pv.Sources = append(pv.Sources, VolumeSource{Kind: "downwardAPI", Name: ""})
			}
		}
	}
	return pv
}

// PodEnvironment returns the environment and mounts of a pod.
func (s *Session) PodEnvironment(ctx context.Context, ns, name string) (PodEnv, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(Resource{Group: "core", Version: "v1", Name: "pods", Namespaced: true}, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return PodEnv{}, err
	}
	var pod corev1.Pod
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &pod); err != nil {
		return PodEnv{}, fmt.Errorf("reading the pod: %w", err)
	}
	return podEnvironment(&pod), nil
}

// maxRefValue bounds what one reveal returns; a key larger than this (a big certificate bundle) is cut and says so.
const maxRefValue = 64 * 1024

// RefValue returns the value of one key of a Secret or ConfigMap, on demand: the pod view never loads values by itself.
// It is only called when the user asks to show a value, and the value is not logged or cached here.
func (s *Session) RefValue(ctx context.Context, ns, kind, name, key string) (string, error) {
	var res Resource
	switch kind {
	case "secret":
		res = Resource{Group: "core", Version: "v1", Name: "secrets", Namespaced: true}
	case "configmap":
		res = Resource{Group: "core", Version: "v1", Name: "configmaps", Namespaced: true}
	default:
		return "", fmt.Errorf("unsupported kind %q", kind)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	obj, err := s.client(res, ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	data, _ := obj.Object["data"].(map[string]any)
	raw, ok := data[key].(string)
	if !ok {
		if kind == "configmap" {
			if bin, _ := obj.Object["binaryData"].(map[string]any); bin != nil {
				if _, has := bin[key]; has {
					return "", fmt.Errorf("%s/%s key %q is binary", kind, name, key)
				}
			}
		}
		return "", fmt.Errorf("%s/%s has no key %q", kind, name, key)
	}
	val := raw
	if kind == "secret" { // the API returns secret data base64-encoded
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return "", fmt.Errorf("decoding %s/%s key %q: %w", kind, name, key, err)
		}
		val = string(b)
	}
	if len(val) > maxRefValue {
		val = val[:maxRefValue] + "\n… (cut at 64 KiB)"
	}
	return val, nil
}
