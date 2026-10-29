package kube

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

type metaAPIResourceList = metav1.APIResourceList

func discoverPreferred(d discovery.DiscoveryInterface) ([]*metav1.APIResourceList, error) {
	return d.ServerPreferredResources()
}

func flattenResources(lists []*metav1.APIResourceList) []Resource {
	var out []Resource
	seen := map[string]bool{}
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if containsSlash(r.Name) || !hasVerb(r.Verbs, "list") || !hasVerb(r.Verbs, "watch") {
				continue // subresources and non-watchable resources
			}
			group := r.Group
			if group == "" {
				group = gv.Group
			}
			version := r.Version
			if version == "" {
				version = gv.Version
			}
			key := resourceKey(group, version, r.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			if group == "" {
				group = "core"
			}
			out = append(out, Resource{
				Key: key, Group: group, Version: version, Name: r.Name, Kind: r.Kind,
				Namespaced: r.Namespaced, ShortNames: r.ShortNames, Verbs: r.Verbs,
			})
		}
	}
	sortResources(out)
	return out
}

func containsSlash(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return true
		}
	}
	return false
}
