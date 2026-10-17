package workbench

import (
	"errors"
	"time"
)

var ErrResolutionRequired = errors.New("issue_resolution_required")
var ErrVerificationRequired = errors.New("issue_verification_required")

// IssueResolutionInput records a buyer's evidence and outcome. It does not
// replace or modify the supplier facts used by the condition evaluator.
type IssueResolutionInput struct {
	ResolutionType    string `json:"resolution_type"`
	ActionTaken       string `json:"action_taken"`
	Result            string `json:"result"`
	Reference         string `json:"reference"`
	SupplierContact   string `json:"supplier_contact"`
	RevisedDate       string `json:"revised_date"`
	ConfirmedQuantity string `json:"confirmed_quantity"`
	FollowUpDate      string `json:"follow_up_date"`
}

type IssueResolution struct {
	ID string `json:"id"`
	IssueResolutionInput
	SubmittedBy      string            `json:"submitted_by"`
	SubmittedAt      time.Time         `json:"submitted_at"`
	MaterialRevision int64             `json:"material_revision"`
	ConditionActive  bool              `json:"condition_active"`
	Rule             string            `json:"rule"`
	Evidence         map[string]string `json:"evidence"`
	QuantityUOM      string            `json:"quantity_uom"`
}

type IssueEvent struct {
	ID               string            `json:"id"`
	ExceptionID      string            `json:"exception_id"`
	Action           string            `json:"action"`
	FromStatus       string            `json:"from_status"`
	ToStatus         string            `json:"to_status"`
	ActorName        string            `json:"actor_name"`
	CreatedAt        time.Time         `json:"created_at"`
	Reason           string            `json:"reason"`
	Resolution       *IssueResolution  `json:"resolution"`
	MaterialRevision int64             `json:"material_revision"`
	ConditionActive  bool              `json:"condition_active"`
	Rule             string            `json:"rule"`
	Evidence         map[string]string `json:"evidence"`
	QuantityUOM      string            `json:"quantity_uom"`
	BeforeWorkflow   *WorkflowInput    `json:"before_workflow"`
	AfterWorkflow    *WorkflowInput    `json:"after_workflow"`
}

// Only this typed payload is stored in the immutable event JSON. The event row
// supplies identity, actor and server timestamp when read back to the client.
type issueEventDetails struct {
	Resolution       *IssueResolutionInput `json:"resolution,omitempty"`
	Reason           string                `json:"reason,omitempty"`
	MaterialRevision int64                 `json:"material_revision"`
	ConditionActive  bool                  `json:"condition_active"`
	Rule             string                `json:"rule"`
	Evidence         map[string]string     `json:"evidence"`
	QuantityUOM      string                `json:"quantity_uom"`
	BeforeWorkflow   *WorkflowInput        `json:"before_workflow,omitempty"`
	AfterWorkflow    *WorkflowInput        `json:"after_workflow,omitempty"`
}
