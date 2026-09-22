package policy

import (
	"maps"
	"slices"
)

func (e *Engine) Enforce(subject, target string, ctx *EvalContext) bool {
	ctx = normalize(ctx)
	if subject == target {
		return true
	}
	visited := e.walk(subject, ctx, target)
	_, ok := visited[target]
	return ok
}

func (e *Engine) Reachable(subject string, ctx *EvalContext) []string {
	ctx = normalize(ctx)
	return slices.Sorted(maps.Keys(e.walk(subject, ctx, "")))
}

// walk performs BFS on the ctx-filtered static graph with a hop limit.
// If stopAt is non-empty, search still records every visited node (needed so
// Enforce and Reachable share one skeleton); callers inspect the set.
func (e *Engine) walk(subject string, ctx *EvalContext, stopAt string) map[string]bool {
	snap := e.current.Load()
	if snap == nil {
		panic("policy: Engine must be created with NewEngine")
	}
	visited := map[string]bool{subject: true}
	type item struct {
		id    string
		depth int
	}
	queue := []item{{subject, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if stopAt != "" && cur.id == stopAt && cur.id != subject {
			return visited
		}
		if cur.depth >= maxDepth {
			continue
		}
		for _, b := range snap.out[cur.id] {
			if !edgeActive(b, ctx) {
				continue
			}
			if visited[b.Dst] {
				continue
			}
			visited[b.Dst] = true
			if stopAt != "" && b.Dst == stopAt {
				return visited
			}
			queue = append(queue, item{b.Dst, cur.depth + 1})
		}
	}
	return visited
}
