package experiment

import "testing"

func BenchmarkDDMin(b *testing.B) {
	items := make([]string, 128)
	for index := range items {
		items[index] = string(rune('a' + index%26))
	}
	for n := 0; n < b.N; n++ {
		_, _, _ = DDMin(items, 1000, func(candidate []string) (bool, error) {
			for _, value := range candidate {
				if value == "z" {
					return true, nil
				}
			}
			return false, nil
		})
	}
}
