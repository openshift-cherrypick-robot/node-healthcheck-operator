package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IsConditionTrue return true when the conditions contain a condition of given type and reason with status true
func IsConditionTrue(conditions []v1.Condition, conditionType string, reason string) bool {
	condition := meta.FindStatusCondition(conditions, conditionType)
	if condition == nil {
		return false
	}
	if condition.Status != v1.ConditionTrue {
		return false
	}
	if condition.Reason != reason {
		return false
	}
	return true
}
