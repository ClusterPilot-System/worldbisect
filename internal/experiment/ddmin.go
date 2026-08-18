package experiment

import "fmt"

type TestFunction func(candidate []string) (bool, error)

func DDMin(items []string, budget int, test TestFunction) ([]string, int, error) {
	current := append([]string(nil), items...)
	if len(current) == 0 {
		return current, 0, nil
	}
	experiments := 0
	n := 2
	for len(current) >= 2 {
		if experiments >= budget {
			return current, experiments, fmt.Errorf("experiment budget exhausted")
		}
		chunks := split(current, n)
		reduced := false
		for _, chunk := range chunks {
			if experiments >= budget {
				return current, experiments, fmt.Errorf("experiment budget exhausted")
			}
			experiments++
			passes, err := test(chunk)
			if err != nil {
				return current, experiments, err
			}
			if passes {
				current = append([]string(nil), chunk...)
				n = max(2, n-1)
				reduced = true
				break
			}
		}
		if reduced {
			continue
		}
		for _, chunk := range chunks {
			complement := subtract(current, chunk)
			if len(complement) == 0 {
				continue
			}
			if experiments >= budget {
				return current, experiments, fmt.Errorf("experiment budget exhausted")
			}
			experiments++
			passes, err := test(complement)
			if err != nil {
				return current, experiments, err
			}
			if passes {
				current = complement
				n = max(2, n-1)
				reduced = true
				break
			}
		}
		if reduced {
			continue
		}
		if n >= len(current) {
			break
		}
		n = min(len(current), n*2)
	}
	return current, experiments, nil
}

func split(items []string, parts int) [][]string {
	if parts > len(items) {
		parts = len(items)
	}
	result := make([][]string, 0, parts)
	base := len(items) / parts
	remainder := len(items) % parts
	start := 0
	for index := 0; index < parts; index++ {
		size := base
		if index < remainder {
			size++
		}
		end := start + size
		result = append(result, append([]string(nil), items[start:end]...))
		start = end
	}
	return result
}

func subtract(all, subset []string) []string {
	set := make(map[string]int)
	for _, item := range subset {
		set[item]++
	}
	result := make([]string, 0, len(all)-len(subset))
	for _, item := range all {
		if set[item] > 0 {
			set[item]--
			continue
		}
		result = append(result, item)
	}
	return result
}
