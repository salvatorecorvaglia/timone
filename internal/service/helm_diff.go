package service

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"timone/internal/integrations"
	"timone/internal/model"
)

func helmDiff(out *model.HelmComparison) {
	out.Changes = []model.HelmChange{}
	out.ConfigurationComparable = out.From.ConfigurationAvailable && out.To.ConfigurationAvailable
	if !out.ConfigurationComparable {
		return
	}
	before, after := map[string]model.HelmManifest{}, map[string]model.HelmManifest{}
	for _, r := range out.From.Configuration {
		before[integrations.InventoryKey(r.Resource)] = r
	}
	for _, r := range out.To.Configuration {
		after[integrations.InventoryKey(r.Resource)] = r
	}
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	ordered := []string{}
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		a, aok := before[key]
		b, bok := after[key]
		r := a.Resource
		if bok {
			r = b.Resource
		}
		var walk func(string, any, any, bool, bool)
		walk = func(path string, a, b any, aok, bok bool) {
			aj, _ := json.Marshal(a)
			bj, _ := json.Marshal(b)
			if aok == bok && string(aj) == string(bj) {
				return
			}
			if len(out.Changes) >= 1000 {
				out.ChangesTruncated = true
				return
			}
			if am, ok := a.(map[string]any); ok {
				if bm, ok := b.(map[string]any); ok {
					fields := map[string]bool{}
					for k := range am {
						fields[k] = true
					}
					for k := range bm {
						fields[k] = true
					}
					ks := []string{}
					for k := range fields {
						ks = append(ks, k)
					}
					sort.Strings(ks)
					for _, k := range ks {
						av, ae := am[k]
						bv, be := bm[k]
						walk(path+"/"+strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1"), av, bv, ae, be)
					}
					return
				}
			}
			if aa, ok := a.([]any); ok {
				if ba, ok := b.([]any); ok {
					for i := 0; i < max(len(aa), len(ba)); i++ {
						var av, bv any
						if i < len(aa) {
							av = aa[i]
						}
						if i < len(ba) {
							bv = ba[i]
						}
						walk(path+"/"+strconv.Itoa(i), av, bv, i < len(aa), i < len(ba))
					}
					return
				}
			}
			change := "changed"
			if !aok {
				change = "added"
			}
			if !bok {
				change = "removed"
			}
			out.Changes = append(out.Changes, model.HelmChange{Resource: r, Path: path, Before: a, After: b, Change: change})
		}
		walk("", map[string]any(a.Object), map[string]any(b.Object), aok, bok)
	}
}
