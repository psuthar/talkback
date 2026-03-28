package prrisk

import "testing"

func TestApplyRiskFloorWorkflowNotLow(t *testing.T) {
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	final, applied, floorMin, _ := applyRiskFloor(6, factors)
	if floorMin != floorMinNotLowBand {
		t.Fatalf("floorMin=%v want %v", floorMin, floorMinNotLowBand)
	}
	if !applied {
		t.Fatal("expected floor applied when net below floor")
	}
	if final != floorMinNotLowBand {
		t.Fatalf("final=%v want %v", final, floorMinNotLowBand)
	}
}

func TestApplyRiskFloorNoRule(t *testing.T) {
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	final, applied, floorMin, reasons := applyRiskFloor(5, factors)
	if floorMin != 0 || applied || len(reasons) != 0 {
		t.Fatalf("unexpected floor: final=%v applied=%v floorMin=%v reasons=%v", final, applied, floorMin, reasons)
	}
	if final != 5 {
		t.Fatalf("final=%v want 5", final)
	}
}

func TestApplyRiskFloorVeryLargeDiff(t *testing.T) {
	factors := []RiskFactor{{ID: "diff_very_large", Points: 22}}
	final, applied, floorMin, _ := applyRiskFloor(2, factors)
	if floorMin != floorMinNotLowBand || !applied || final != floorMinNotLowBand {
		t.Fatalf("got final=%v applied=%v floorMin=%v", final, applied, floorMin)
	}
}

func TestApplyRiskFloorLargeDiff(t *testing.T) {
	factors := []RiskFactor{{ID: "diff_large", Points: 12}}
	final, applied, floorMin, reasons := applyRiskFloor(5, factors)
	if floorMin != floorMinNotLowBand {
		t.Fatalf("floorMin=%v want %v", floorMin, floorMinNotLowBand)
	}
	if !applied {
		t.Fatal("expected floor applied for diff_large when net below floor")
	}
	if final != floorMinNotLowBand {
		t.Fatalf("final=%v want %v", final, floorMinNotLowBand)
	}
	if len(reasons) == 0 {
		t.Fatal("expected at least one floor reason for diff_large")
	}
}

func TestApplyRiskFloorManyFiles(t *testing.T) {
	factors := []RiskFactor{{ID: "many_files", Points: 14}}
	final, applied, floorMin, _ := applyRiskFloor(3, factors)
	if floorMin != floorMinNotLowBand || !applied || final != floorMinNotLowBand {
		t.Fatalf("many_files floor: final=%v applied=%v floorMin=%v", final, applied, floorMin)
	}
}

func TestApplyRiskFloorDeployConfig(t *testing.T) {
	factors := []RiskFactor{{ID: "deploy_config", Points: 12}}
	final, applied, floorMin, _ := applyRiskFloor(8, factors)
	if floorMin != floorMinNotLowBand || !applied || final != floorMinNotLowBand {
		t.Fatalf("deploy_config floor: final=%v applied=%v floorMin=%v", final, applied, floorMin)
	}
}

func TestApplyRiskFloorGoModDeps(t *testing.T) {
	factors := []RiskFactor{{ID: "go_mod_deps", Points: 8}}
	final, applied, floorMin, _ := applyRiskFloor(4, factors)
	if floorMin != floorMinNotLowBand || !applied || final != floorMinNotLowBand {
		t.Fatalf("go_mod_deps floor: final=%v applied=%v floorMin=%v", final, applied, floorMin)
	}
}

func TestApplyRiskFloorPresentButNotApplied(t *testing.T) {
	// Floor rule fires (ci_workflows) but net is already at or above the floor.
	// FloorMinScore must be set, but FloorApplied must be false.
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	net := floorMinNotLowBand + 5 // above floor
	final, applied, floorMin, reasons := applyRiskFloor(net, factors)
	if floorMin != floorMinNotLowBand {
		t.Fatalf("floorMin=%v want %v", floorMin, floorMinNotLowBand)
	}
	if applied {
		t.Fatal("expected FloorApplied=false when net already above floor")
	}
	if final != net {
		t.Fatalf("final=%v want %v (net unchanged)", final, net)
	}
	if len(reasons) == 0 {
		t.Fatal("expected floor reasons even when floor not applied")
	}
}
