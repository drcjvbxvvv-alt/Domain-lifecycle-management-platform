package release

import "sort"

// ShardStrategy controls how release domains are split into shards.
type ShardStrategy string

const (
	// ShardStrategyByHostGroup (default): one shard per unique host_group_id.
	// Domains with no host_group are collected into a trailing "default" shard.
	ShardStrategyByHostGroup ShardStrategy = "by_host_group"

	// ShardStrategyByRegion: one shard per agent region tag.
	// Deferred to Phase 3; falls through to by_host_group in P2.
	ShardStrategyByRegion ShardStrategy = "by_region"
)

// domainPlanInput is the per-domain info the planner needs.
type domainPlanInput struct {
	ID          int64
	HostGroupID *int64
}

// PlannedShard is the output of PlanShards for one shard.
type PlannedShard struct {
	ShardIndex  int
	DomainIDs   []int64
	HostGroupID *int64 // nil = no/mixed host_group
}

// PlanShards splits domains into an ordered slice of PlannedShards using the
// given strategy. Output is sorted by ShardIndex and is deterministic given
// the same input.
//
// P2 supports: by_host_group. by_region falls through to by_host_group.
func PlanShards(strategy ShardStrategy, domains []domainPlanInput) []PlannedShard {
	// All P2 strategies use host_group splitting.
	return planByHostGroup(domains)
}

// planByHostGroup groups domains into one shard per unique host_group_id.
// Shards are ordered by host_group_id ascending for determinism.
// Domains with nil/zero host_group_id are collected into a trailing default shard.
func planByHostGroup(domains []domainPlanInput) []PlannedShard {
	groups := make(map[int64][]int64) // host_group_id → domain IDs
	var noGroup []int64

	for _, d := range domains {
		if d.HostGroupID != nil && *d.HostGroupID > 0 {
			groups[*d.HostGroupID] = append(groups[*d.HostGroupID], d.ID)
		} else {
			noGroup = append(noGroup, d.ID)
		}
	}

	// Sort host_group_ids for deterministic shard ordering.
	hgIDs := make([]int64, 0, len(groups))
	for hgID := range groups {
		hgIDs = append(hgIDs, hgID)
	}
	sort.Slice(hgIDs, func(i, j int) bool { return hgIDs[i] < hgIDs[j] })

	shards := make([]PlannedShard, 0, len(hgIDs)+1)
	for i, hgID := range hgIDs {
		hgIDCopy := hgID
		shards = append(shards, PlannedShard{
			ShardIndex:  i,
			DomainIDs:   groups[hgID],
			HostGroupID: &hgIDCopy,
		})
	}

	// Domains with no host_group go in the last shard.
	if len(noGroup) > 0 {
		shards = append(shards, PlannedShard{
			ShardIndex:  len(shards),
			DomainIDs:   noGroup,
			HostGroupID: nil,
		})
	}

	// Edge case: caller validated non-empty domains; this branch should not fire.
	if len(shards) == 0 {
		return []PlannedShard{{ShardIndex: 0}}
	}
	return shards
}
