package services

import (
	"module-a/models"
	"module-a/repository"
)

// GetRepo exposes the repository for test helpers.
func (e *WorkflowEngine) GetRepo() *repository.FirestoreRepository {
	return e.repo
}

// EvaluateWorkflowPublic is the exported version of evaluateWorkflow,
// used by the handler's test/simulate endpoints where we want condition
// results without executing any actions.
func (e *WorkflowEngine) EvaluateWorkflowPublic(wf *models.Workflow, order *models.Order, lines []models.OrderLine) (models.WorkflowEvalResult, bool) {
	return e.evaluateWorkflow(wf, order, lines)
}
