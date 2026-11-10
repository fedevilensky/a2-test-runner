package main

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestDiffOutputLines(t *testing.T) {
	cases := []struct {
		name, expected, actual, diff string
		left, right                  []outputLine
		ok                           bool
	}{
		{"replace", "same\nold\nlast\n", "same\nnew\nlast\n", "--- expected\n+++ actual\n@@ -1,3 +1,3 @@\n same\n-old\n+new\n last\n", []outputLine{{Text: "same"}, {Text: "old", Changed: true}, {Text: "last"}}, []outputLine{{Text: "same"}, {Text: "new", Changed: true}, {Text: "last"}}, true},
		{"insert into empty", "", "new\n", "@@ -0,0 +1 @@\n+new\n", nil, []outputLine{{Text: "new", Changed: true}}, true},
		{"delete without newline", "old", "", "@@ -1 +0,0 @@\n-old\n\\ No newline at end of file\n", []outputLine{{Text: "old", Changed: true}}, nil, true},
		{"invalid hunk", "old\n", "new\n", "@@ -8 +1 @@\n-old\n+new\n", nil, nil, false},
		{"unavailable diff", "old", "new", "", nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			left, right, ok := diffOutputLines(tc.expected, tc.actual, tc.diff)
			if ok != tc.ok || (ok && (!reflect.DeepEqual(left, tc.left) || !reflect.DeepEqual(right, tc.right))) {
				t.Fatalf("got %#v, %#v, %v", left, right, ok)
			}
		})
	}
}

func TestDiffOutputLinesWithSystemDiff(t *testing.T) {
	if _, err := exec.LookPath("diff"); err != nil {
		t.Skip("diff unavailable")
	}
	expected := "first\n" + strings.Repeat("unchanged\n", 12) + "old\n"
	actual := "first\n" + strings.Repeat("unchanged\n", 12) + "new\n"
	left, right, ok := diffOutputLines(expected, actual, unifiedDiff(expected, actual))
	if !ok || !left[len(left)-1].Changed || !right[len(right)-1].Changed || left[0].Changed || right[0].Changed {
		t.Fatalf("incorrect diff mapping: %#v %#v", left, right)
	}
}
