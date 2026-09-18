package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSkeletonConditionIsExplicitlyNotReady(t *testing.T) {
	condition := skeletonCondition(7)
	if condition.Type != ConditionReady {
		t.Fatalf("condition type = %q, want %q", condition.Type, ConditionReady)
	}
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("condition status = %q, want %q", condition.Status, metav1.ConditionFalse)
	}
	if condition.Reason != ReasonPhase1Skeleton {
		t.Fatalf("condition reason = %q, want %q", condition.Reason, ReasonPhase1Skeleton)
	}
	if condition.ObservedGeneration != 7 {
		t.Fatalf("observed generation = %d, want 7", condition.ObservedGeneration)
	}
}

func TestReadyConditionPresentOnlyAcceptsTrue(t *testing.T) {
	if readyConditionPresent(nil) {
		t.Fatal("nil conditions must not be ready")
	}
	if readyConditionPresent([]metav1.Condition{{Type: ConditionReady, Status: metav1.ConditionFalse}}) {
		t.Fatal("false Ready condition must not be ready")
	}
	if !readyConditionPresent([]metav1.Condition{{Type: ConditionReady, Status: metav1.ConditionTrue}}) {
		t.Fatal("true Ready condition must be ready")
	}
}
