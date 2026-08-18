package experiment

import (
	"reflect"
	"testing"
)

func TestDDMin(t *testing.T) {
	result, experiments, err := DDMin([]string{"a", "b", "c", "d"}, 100, func(candidate []string) (bool, error) {
		set := make(map[string]bool)
		for _, value := range candidate {
			set[value] = true
		}
		return set["b"] && set["d"], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, []string{"b", "d"}) {
		t.Fatalf("result = %#v", result)
	}
	if experiments == 0 {
		t.Fatal("no experiments")
	}
}
