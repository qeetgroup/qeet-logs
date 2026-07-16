// Package lifecycle is the pure planning core for the cold-tier storage
// lifecycle (PRD Module 6 hot/warm/cold tiering / P2-G2). It contains NO I/O:
// given a snapshot of ClickHouse partitions (their table, tenant, current
// volume, and age in days) and each tenant's tier configuration, it computes the
// set of partition MOVEs that should be issued to shift aged data from the hot
// (local) volume to the cold (S3/MinIO) volume.
//
// The ClickHouse per-record delete TTL (clickhouse/migrations/0009_cold_tier.sql)
// still owns DELETION at the retention boundary — this planner never plans a
// delete; it only plans hot→cold moves ahead of that. cmd/lifecycle wires this
// pure planner to the real ClickHouse `system.parts` snapshot and issues the
// `ALTER TABLE … MOVE PARTITION … TO VOLUME 'cold'` statements.
package lifecycle

import "sort"

// Volume names, matching the `hot_cold` storage policy in
// clickhouse/config/storage.xml.
const (
	VolumeHot  = "hot"
	VolumeCold = "cold"
)

// Tier is the storage tier a partition SHOULD live on given its age.
type Tier string

const (
	// TierHot: recent data, kept on the fast local disk.
	TierHot Tier = "hot"
	// TierCold: aged past the hot window, belongs on cheap object storage.
	TierCold Tier = "cold"
	// TierExpired: past the retention boundary — left to the ClickHouse delete
	// TTL, never moved by the planner.
	TierExpired Tier = "expired"
)

// TenantTier is a tenant's tiering configuration (mirrors the tenant_tiers
// table). HotDays is the local-disk window; ColdDays is the retention boundary
// after which data is deleted (by the ClickHouse TTL, not by this planner).
type TenantTier struct {
	TenantID string
	HotDays  int
	ColdDays int
}

// Classify returns the tier a partition of the given age (in days) should be on.
// A ColdDays of 0 disables the retention boundary (never expired via this path).
func (t TenantTier) Classify(ageDays int) Tier {
	if t.ColdDays > 0 && ageDays >= t.ColdDays {
		return TierExpired
	}
	if ageDays >= t.HotDays {
		return TierCold
	}
	return TierHot
}

// Partition is one observed ClickHouse partition (from system.parts), collapsed
// to what the planner needs.
type Partition struct {
	Table         string // e.g. "logs"
	Partition     string // partition id as ClickHouse reports it (partition_id)
	TenantID      string // parsed from the (tenant_id, month) partition key
	CurrentVolume string // "hot" | "cold" — the disk volume the part is on now
	AgeDays       int    // age of the partition's newest data, in days
}

// Move is a planned partition relocation to a target volume.
type Move struct {
	Table     string `json:"table"`
	Partition string `json:"partition"`
	TenantID  string `json:"tenant_id"`
	ToVolume  string `json:"to_volume"`
	Reason    string `json:"reason"`
}

// MovePlan computes the hot→cold moves for the observed partitions. A partition
// is planned for a move only when it is currently on the hot volume AND its age
// classifies it as cold (past the tenant's hot window but not yet expired).
// Partitions already on cold, still hot, or expired are left untouched. The
// per-tenant config is looked up in tiers; tenants absent from the map use def.
// Output is deterministic (sorted by table, then partition, then tenant).
func MovePlan(parts []Partition, tiers map[string]TenantTier, def TenantTier) []Move {
	var moves []Move
	for _, p := range parts {
		if p.CurrentVolume == VolumeCold {
			continue // already cold — nothing to do
		}
		cfg, ok := tiers[p.TenantID]
		if !ok {
			cfg = def
			cfg.TenantID = p.TenantID
		}
		if cfg.Classify(p.AgeDays) == TierCold {
			moves = append(moves, Move{
				Table:     p.Table,
				Partition: p.Partition,
				TenantID:  p.TenantID,
				ToVolume:  VolumeCold,
				Reason:    reason(p.AgeDays, cfg.HotDays),
			})
		}
	}
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].Table != moves[j].Table {
			return moves[i].Table < moves[j].Table
		}
		if moves[i].Partition != moves[j].Partition {
			return moves[i].Partition < moves[j].Partition
		}
		return moves[i].TenantID < moves[j].TenantID
	})
	return moves
}

func reason(ageDays, hotDays int) string {
	return "age " + itoa(ageDays) + "d ≥ hot window " + itoa(hotDays) + "d"
}

// itoa avoids a strconv import for a single positive-int format.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
