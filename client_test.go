package gia

import "testing"

func TestTableClassification(t *testing.T) {
	t.Parallel()

	table, present, err := tableFromLabels([]string{"docs", "gia/table/skill"})
	if err != nil || !present || table != "skill" {
		t.Fatalf("tableFromLabels() = %q, %v, %v", table, present, err)
	}

	_, _, err = tableFromLabels([]string{"gia/table/skill", "gia/table/task"})
	if !IsCode(err, CodeInvalidResource) {
		t.Fatalf("expected invalid_resource, got %v", err)
	}

	_, _, err = tableFromLabels([]string{"gia/table/skill", "gia/table/skill"})
	if !IsCode(err, CodeInvalidResource) {
		t.Fatalf("expected duplicate table label to be invalid_resource, got %v", err)
	}

	_, present, err = tableFromLabels([]string{"docs"})
	if err != nil || present {
		t.Fatalf("ordinary issue classified as resource: %v, %v", present, err)
	}
}
