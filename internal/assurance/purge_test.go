package assurance

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/contracts"
	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
	"example.com/solo-0021-metrology-assurance-suite/internal/repository"
)

func TestPurgeWorkflowEndToEnd(t *testing.T) {
	ctx := context.Background()
	service := NewService(repository.New())

	sealedCase := func(tag string) string {
		device, err := service.RegisterDevice(ctx, contracts.RegisterDeviceRequest{
			AssetTag: tag, Model: "Bench Meter", Room: "Room A", Steward: "Lin",
		})
		if err != nil {
			t.Fatalf("register %s: %v", tag, err)
		}
		opened, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
			DeviceID: device.ID, Nominal: 25.0, Tolerance: 0.5, Steward: "Qiao",
		})
		if err != nil {
			t.Fatalf("open case for %s: %v", tag, err)
		}
		recorded, err := service.RecordMeasurement(ctx, opened.Case.ID, contracts.RecordMeasurementRequest{
			Observed: 25.1, Technician: "Rui",
		})
		if err != nil {
			t.Fatalf("measure for %s: %v", tag, err)
		}
		if recorded.Case.State != domain.CaseReviewReady {
			t.Fatalf("case for %s state = %s", tag, recorded.Case.State)
		}
		sealed, err := service.SealCase(ctx, opened.Case.ID, contracts.SealCaseRequest{
			Reviewer: "Verifier", Summary: "accepted and archived",
		})
		if err != nil {
			t.Fatalf("seal for %s: %v", tag, err)
		}
		if sealed.Case.State != domain.CaseSealed {
			t.Fatalf("sealed case for %s state = %s", tag, sealed.Case.State)
		}
		return sealed.Case.ID
	}

	// Two sealed cases that are past retention once the cutoff moves forward.
	oldSealedA := sealedCase("MET-3001")
	oldSealedB := sealedCase("MET-3002")

	// One case still awaiting measurement, with no measurement at all.
	awaitingDevice, err := service.RegisterDevice(ctx, contracts.RegisterDeviceRequest{
		AssetTag: "MET-3003", Model: "Bench Meter", Room: "Room A", Steward: "Lin",
	})
	if err != nil {
		t.Fatalf("register MET-3003: %v", err)
	}
	awaiting, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
		DeviceID: awaitingDevice.ID, Nominal: 25.0, Tolerance: 0.5, Steward: "Qiao",
	})
	if err != nil {
		t.Fatalf("open awaiting case: %v", err)
	}

	// One case in recheck after a rejected measurement: it must keep its
	// measurement and remain reworkable.
	recheckDevice, err := service.RegisterDevice(ctx, contracts.RegisterDeviceRequest{
		AssetTag: "MET-3004", Model: "Bench Meter", Room: "Room A", Steward: "Lin",
	})
	if err != nil {
		t.Fatalf("register MET-3004: %v", err)
	}
	recheck, err := service.OpenCase(ctx, contracts.OpenCaseRequest{
		DeviceID: recheckDevice.ID, Nominal: 25.0, Tolerance: 0.5, Steward: "Qiao",
	})
	if err != nil {
		t.Fatalf("open recheck case: %v", err)
	}
	if _, err := service.RecordMeasurement(ctx, recheck.Case.ID, contracts.RecordMeasurementRequest{
		Observed: 26.5, Technician: "Rui",
	}); err != nil {
		t.Fatalf("reject measurement: %v", err)
	}

	// A cutoff in the past releases nothing: nothing is sealed before yesterday.
	past, err := service.Purge(ctx, contracts.PurgeRequest{
		SealedBefore: time.Now().Add(-time.Hour), Actor: "steward",
	})
	if err != nil {
		t.Fatalf("past cutoff purge: %v", err)
	}
	if past.PurgedCases != 0 {
		t.Fatalf("past cutoff purged %d cases", past.PurgedCases)
	}
	for _, id := range []string{oldSealedA, oldSealedB, awaiting.Case.ID, recheck.Case.ID} {
		if _, err := service.Case(ctx, id); err != nil {
			t.Fatalf("case %s vanished after null sweep: %v", id, err)
		}
	}
	for _, id := range []string{oldSealedA, oldSealedB} {
		if _, err := service.Evidence(ctx, id); err != nil {
			t.Fatalf("evidence %s vanished after null sweep: %v", id, err)
		}
	}

	// A cutoff in the future is past both seal times, but unsealed work remains.
	swept, err := service.Purge(ctx, contracts.PurgeRequest{
		SealedBefore: time.Now().Add(time.Hour), Actor: "steward",
	})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if swept.PurgedCases != 2 || swept.PurgedMeasurements != 2 || swept.PurgedSeals != 2 || swept.PurgedEvents != 6 {
		t.Fatalf("purge stats = %+v, want 2/2/2/6", swept)
	}
	if len(swept.CaseIDs) != 2 {
		t.Fatalf("purged case ids = %v", swept.CaseIDs)
	}

	for _, id := range []string{oldSealedA, oldSealedB} {
		if _, err := service.Case(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("case %s read after release: err=%v", id, err)
		}
		// The associated evidence view must report the same absence: no orphan
		// seals, measurements, or audit events retrievable by case id.
		if _, err := service.Evidence(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("evidence %s readable after release: err=%v", id, err)
		}
	}

	// Device files are never part of a case release.
	if _, err := service.Device(ctx, awaitingDevice.ID); err != nil {
		t.Errorf("awaiting device removed: %v", err)
	}
	if _, err := service.Device(ctx, recheckDevice.ID); err != nil {
		t.Errorf("recheck device removed: %v", err)
	}

	// The awaiting case can still be measured and then sealed.
	continued, err := service.RecordMeasurement(ctx, awaiting.Case.ID, contracts.RecordMeasurementRequest{
		Observed: 25.0, Technician: "Rui",
	})
	if err != nil {
		t.Fatalf("continue awaiting case: %v", err)
	}
	if continued.Case.State != domain.CaseReviewReady {
		t.Fatalf("continued case state = %s", continued.Case.State)
	}
	if _, err := service.SealCase(ctx, awaiting.Case.ID, contracts.SealCaseRequest{
		Reviewer: "Verifier", Summary: "sealed after sweep",
	}); err != nil {
		t.Fatalf("seal continued case: %v", err)
	}

	// The recheck case keeps its rejected measurement, can be reopened,
	// re-measured, and sealed.
	view, err := service.Case(ctx, recheck.Case.ID)
	if err != nil {
		t.Fatalf("read recheck case: %v", err)
	}
	if view.Case.State != domain.CaseRecheckRequired || len(view.Measurements) != 1 {
		t.Fatalf("recheck case altered: state=%s measurements=%d", view.Case.State, len(view.Measurements))
	}
	reopened, err := service.ReopenCase(ctx, recheck.Case.ID, contracts.ReopenCaseRequest{
		Actor: "Lin", Reason: "repeat after retention sweep",
	})
	if err != nil {
		t.Fatalf("reopen recheck case: %v", err)
	}
	if len(reopened.Measurements) != 1 {
		t.Fatalf("reopen lost measurement history: %d", len(reopened.Measurements))
	}
	if _, err := service.RecordMeasurement(ctx, recheck.Case.ID, contracts.RecordMeasurementRequest{
		Observed: 25.05, Technician: "Rui",
	}); err != nil {
		t.Fatalf("remeasure reopened case: %v", err)
	}
	if _, err := service.SealCase(ctx, recheck.Case.ID, contracts.SealCaseRequest{
		Reviewer: "Verifier", Summary: "resealed after rework",
	}); err != nil {
		t.Fatalf("seal reworked case: %v", err)
	}

	// A second sweep leaves the two newly sealed cases alone only when the
	// cutoff precedes their seal time, proving cutoff still gates on sealing.
	again, err := service.Purge(ctx, contracts.PurgeRequest{
		SealedBefore: time.Now().Add(-time.Hour), Actor: "steward",
	})
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if again.PurgedCases != 0 {
		t.Fatalf("second sweep purged %d freshly sealed cases", again.PurgedCases)
	}
	for _, id := range []string{awaiting.Case.ID, recheck.Case.ID} {
		if _, err := service.Case(ctx, id); err != nil {
			t.Errorf("newly sealed case %s lost: %v", id, err)
		}
		evidence, err := service.Evidence(ctx, id)
		if err != nil {
			t.Errorf("newly sealed evidence %s lost: %v", id, err)
			continue
		}
		if evidence.Seal.CaseID != id {
			t.Errorf("evidence seal case id = %s, want %s", evidence.Seal.CaseID, id)
		}
	}
}
