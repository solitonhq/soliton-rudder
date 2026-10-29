package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ObservedState is the health of one object at one moment, as the flight recorder sees it.
type ObservedState struct {
	Key       string `json:"key"` // Kind/namespace/name
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"` // ok | warn | error
	Detail    string `json:"detail"`
}

// ObservedEvent is a Kubernetes Event worth keeping.
type ObservedEvent struct {
	UID       string    `json:"uid"`
	Count     int       `json:"count"`
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // Normal | Warning
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	At        time.Time `json:"at"`
}

type Observation struct {
	States []ObservedState
	Events []ObservedEvent
	Notes  []string // parts that could not be read
	// Unread lists the object kinds that could not be read this time: their absence means "unknown", not "deleted".
	Unread []string
}

type observeInput struct {
	Pods, Nodes, Workloads, Events []unstructured.Unstructured
}

var eventRes = Resource{Group: "core", Version: "v1", Name: "events", Namespaced: true}

// Normal events that explain what happened; the rest (Pulled, Created, Started…) is noise for a flight recorder.
var keepNormalReasons = map[string]bool{
	"ScalingReplicaSet": true, "SuccessfulCreate": true, "SuccessfulDelete": true, "Killing": true,
	"Scheduled": true, "NodeReady": true, "NodeNotReady": true, "Evicted": true, "Preempted": true,
	"RegisteredNode": true, "RemovingNode": true,
}

// Observe reads the health of pods, nodes and workloads and the recent events. namespaces empty = all.
func (s *Session) Observe(ctx context.Context, namespaces []string) (Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
	defer cancel()
	var in observeInput
	var notes []string
	scopes := namespaces
	if len(scopes) == 0 {
		scopes = []string{""}
	}
	var unread []string
	list := func(r Resource, dst *[]unstructured.Unstructured, required bool, setKind string) error {
		var acc []unstructured.Unstructured
		for _, ns := range scopes {
			if !r.Namespaced {
				ns = ""
			}
			items, err := s.ListScopedStrict(ctx, r, ns, metav1.ListOptions{})
			if err != nil {
				if required {
					return fmt.Errorf("reading %s: %w", r.Name, err)
				}
				notes = append(notes, fmt.Sprintf("%s unavailable: %s", r.Name, shortErr(err)))
				if setKind != "" {
					unread = append(unread, setKind)
				}
				return nil // a partly read kind is discarded: half a list would look like deletions
			}
			for i := range items {
				if setKind != "" {
					items[i].SetKind(setKind)
				}
			}
			acc = append(acc, items...)
			if !r.Namespaced {
				break
			}
		}
		*dst = append(*dst, acc...)
		return nil
	}
	if err := list(podRes, &in.Pods, true, ""); err != nil {
		return Observation{}, err
	}
	_ = list(nodeRes, &in.Nodes, false, "Node")
	for _, w := range topoWorkloadKinds {
		if w.Kind == "CronJob" {
			continue
		}
		_ = list(w.Res, &in.Workloads, false, w.Kind)
	}
	_ = list(eventRes, &in.Events, false, "")
	o := observe(in)
	o.Notes, o.Unread = notes, unread
	return o, nil
}

func podRestarts(p *unstructured.Unstructured) int64 {
	var n int64
	css, _, _ := unstructured.NestedSlice(p.Object, "status", "containerStatuses")
	for _, c := range css {
		cm, _ := c.(map[string]any)
		if v, ok := cm["restartCount"].(int64); ok {
			n += v
		}
	}
	return n
}

func observe(in observeInput) Observation {
	var o Observation
	add := func(kind, ns, name, status, detail string) {
		o.States = append(o.States, ObservedState{Key: kind + "/" + ns + "/" + name, Kind: kind, Namespace: ns, Name: name, Status: status, Detail: detail})
	}
	for i := range in.Pods {
		p := &in.Pods[i]
		st, why := podStatus(p)
		if st == "unknown" {
			st = "warn"
		}
		if n := podRestarts(p); n > 0 {
			why += fmt.Sprintf(" · %d restarts", n)
		}
		add("Pod", p.GetNamespace(), p.GetName(), st, why)
	}
	for i := range in.Nodes {
		st, d := nodeStatus(&in.Nodes[i])
		add("Node", "", in.Nodes[i].GetName(), st, d)
	}
	for i := range in.Workloads {
		w := &in.Workloads[i]
		st, d := workloadState(w)
		add(w.GetKind(), w.GetNamespace(), w.GetName(), st, d)
	}
	sort.Slice(o.States, func(i, j int) bool { return o.States[i].Key < o.States[j].Key })

	for i := range in.Events {
		e := &in.Events[i]
		typ := str(nested(e.Object, "type"))
		reason := str(nested(e.Object, "reason"))
		if typ != "Warning" && !keepNormalReasons[reason] {
			continue
		}
		at := eventTime(e)
		if at.IsZero() {
			continue
		}
		count, _ := nested(e.Object, "count").(int64)
		if count == 0 {
			count = 1
		}
		io, _, _ := unstructured.NestedMap(e.Object, "involvedObject")
		ns := str(io["namespace"])
		if ns == "" {
			ns = e.GetNamespace()
		}
		msg := strings.TrimSpace(str(nested(e.Object, "message")))
		if len(msg) > 600 {
			msg = msg[:600] + "…"
		}
		o.Events = append(o.Events, ObservedEvent{
			UID: string(e.GetUID()), Count: int(count), Namespace: ns, Kind: str(io["kind"]), Name: str(io["name"]),
			Type: typ, Reason: reason, Message: msg, At: at,
		})
	}
	sort.SliceStable(o.Events, func(i, j int) bool { return o.Events[i].At.Before(o.Events[j].At) })
	return o
}

func eventTime(e *unstructured.Unstructured) time.Time {
	for _, f := range [][]string{{"lastTimestamp"}, {"series", "lastObservedTime"}, {"eventTime"}, {"firstTimestamp"}, {"metadata", "creationTimestamp"}} {
		if s, ok := nested(e.Object, f...).(string); ok && s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}
