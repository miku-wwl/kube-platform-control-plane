package controller

import (
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionReady       = "Ready"
	ReasonPhase1Skeleton = "Phase1Skeleton"
)

func skeletonCondition(generation int64) metav1.Condition {
	return metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: generation,
		Reason:             ReasonPhase1Skeleton,
		Message:            "Phase 1 control-plane skeleton is installed; lifecycle orchestration is not active yet.",
		LastTransitionTime: metav1.Now(),
	}
}

func readyConditionPresent(conditions []metav1.Condition) bool {
	return apiMeta.IsStatusConditionTrue(conditions, ConditionReady)
}

func skeletonConditionCurrent(conditions []metav1.Condition, generation int64) bool {
	condition := apiMeta.FindStatusCondition(conditions, ConditionReady)
	return condition != nil &&
		condition.Status == metav1.ConditionFalse &&
		condition.ObservedGeneration == generation &&
		condition.Reason == ReasonPhase1Skeleton
}
