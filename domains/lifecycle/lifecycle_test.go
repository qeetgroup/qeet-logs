package lifecycle

import "testing"

func TestClassify(t *testing.T) {
	cfg := TenantTier{TenantID: "t1", HotDays: 3, ColdDays: 30}
	cases := []struct {
		age  int
		want Tier
	}{
		{0, TierHot},
		{2, TierHot},
		{3, TierCold}, // exactly at the hot boundary → cold
		{29, TierCold},
		{30, TierExpired}, // exactly at retention → expired (delete TTL owns it)
		{100, TierExpired},
	}
	for _, c := range cases {
		if got := cfg.Classify(c.age); got != c.want {
			t.Errorf("Classify(%d) = %q, want %q", c.age, got, c.want)
		}
	}
}

func TestClassifyNoRetention(t *testing.T) {
	cfg := TenantTier{HotDays: 3, ColdDays: 0} // 0 = no delete boundary
	if got := cfg.Classify(10_000); got != TierCold {
		t.Errorf("with ColdDays=0 an ancient partition must be cold, got %q", got)
	}
}

func TestMovePlanOnlyMovesAgedHotPartitions(t *testing.T) {
	def := TenantTier{HotDays: 3, ColdDays: 30}
	tiers := map[string]TenantTier{
		"t-keep-hot": {TenantID: "t-keep-hot", HotDays: 90, ColdDays: 365}, // long hot window
	}
	parts := []Partition{
		{Table: "logs", Partition: "p-new", TenantID: "t1", CurrentVolume: VolumeHot, AgeDays: 1},           // still hot
		{Table: "logs", Partition: "p-aged", TenantID: "t1", CurrentVolume: VolumeHot, AgeDays: 10},         // → move
		{Table: "logs", Partition: "p-cold", TenantID: "t1", CurrentVolume: VolumeCold, AgeDays: 10},        // already cold
		{Table: "logs", Partition: "p-expired", TenantID: "t1", CurrentVolume: VolumeHot, AgeDays: 40},      // expired → skip
		{Table: "metrics", Partition: "p-aged-m", TenantID: "t1", CurrentVolume: VolumeHot, AgeDays: 5},     // → move
		{Table: "logs", Partition: "p-keep", TenantID: "t-keep-hot", CurrentVolume: VolumeHot, AgeDays: 10}, // long hot window → skip
	}

	moves := MovePlan(parts, tiers, def)

	if len(moves) != 2 {
		t.Fatalf("expected 2 moves, got %d: %+v", len(moves), moves)
	}
	// Deterministic order: metrics before logs? No — sorted by table asc: "logs" < "metrics".
	if moves[0].Table != "logs" || moves[0].Partition != "p-aged" {
		t.Errorf("first move = %+v, want logs/p-aged", moves[0])
	}
	if moves[1].Table != "metrics" || moves[1].Partition != "p-aged-m" {
		t.Errorf("second move = %+v, want metrics/p-aged-m", moves[1])
	}
	for _, m := range moves {
		if m.ToVolume != VolumeCold {
			t.Errorf("move target = %q, want cold", m.ToVolume)
		}
	}
}

func TestMovePlanEmpty(t *testing.T) {
	if got := MovePlan(nil, nil, TenantTier{HotDays: 3, ColdDays: 30}); got != nil {
		t.Errorf("nil partitions → nil plan, got %+v", got)
	}
}
