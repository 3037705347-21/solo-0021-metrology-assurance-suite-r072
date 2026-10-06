package contracts

import (
	"time"

	"example.com/solo-0021-metrology-assurance-suite/internal/domain"
)

type RegisterDeviceRequest struct {
	AssetTag string `json:"asset_tag"`
	Model    string `json:"model"`
	Room     string `json:"room"`
	Steward  string `json:"steward"`
}

type OpenCaseRequest struct {
	DeviceID  string  `json:"device_id"`
	Nominal   float64 `json:"nominal"`
	Tolerance float64 `json:"tolerance"`
	Steward   string  `json:"steward"`
}

type RecordMeasurementRequest struct {
	Observed   float64 `json:"observed"`
	Technician string  `json:"technician"`
}

type ReopenCaseRequest struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
}

type SealCaseRequest struct {
	Reviewer string `json:"reviewer"`
	Summary  string `json:"summary"`
}

type PurgeRequest struct {
	SealedBefore time.Time `json:"sealed_before"`
	Actor        string    `json:"actor"`
}

type PurgeResponse struct {
	PurgedCases        int      `json:"purged_cases"`
	PurgedMeasurements int      `json:"purged_measurements"`
	PurgedSeals        int      `json:"purged_seals"`
	PurgedEvents       int      `json:"purged_events"`
	CaseIDs            []string `json:"case_ids"`
}

type EvidenceResponse struct {
	Seal         domain.EvidenceSeal  `json:"seal"`
	Measurements []domain.Measurement `json:"measurements"`
	Events       []domain.AuditEvent  `json:"events"`
}

type DeviceResponse struct {
	Device domain.Device `json:"device"`
}

type CaseResponse struct {
	Case         domain.VerificationCase `json:"case"`
	Measurements []domain.Measurement    `json:"measurements"`
	Seal         *domain.EvidenceSeal    `json:"seal,omitempty"`
	Events       []domain.AuditEvent     `json:"events"`
}

type EventsResponse struct {
	Events []domain.AuditEvent `json:"events"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
