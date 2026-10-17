package workbench

import "time"

type Connection struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	SupplierID            *string    `json:"supplier_id"`
	BuyerUserID           *string    `json:"buyer_user_id"`
	Method                string     `json:"method"`
	Revision              int64      `json:"revision"`
	Paused                bool       `json:"paused"`
	MinimumCadenceSeconds int        `json:"minimum_cadence_seconds"`
	IsDemoDefault         bool       `json:"is_demo_default"`
	Runnable              bool       `json:"runnable"`
	Connector             string     `json:"connector"`
	Origin                string     `json:"origin"`
	Status                string     `json:"status"`
	SourceRevision        int64      `json:"source_revision"`
	LastAttemptAt         *time.Time `json:"last_attempt_at"`
	LastSuccessAt         *time.Time `json:"last_success_at"`
	LastFailureCode       *string    `json:"last_failure_code"`
	ScheduleEnabled       bool       `json:"schedule_enabled"`
	CadenceSeconds        int        `json:"cadence_seconds"`
	NextDueAt             *time.Time `json:"next_due_at"`
}

type ConnectionInput struct {
	Name                  string  `json:"name"`
	SupplierID            *string `json:"supplier_id"`
	BuyerUserID           *string `json:"buyer_user_id"`
	Method                string  `json:"method"`
	MinimumCadenceSeconds int     `json:"minimum_cadence_seconds"`
	CadenceSeconds        int     `json:"cadence_seconds"`
}
