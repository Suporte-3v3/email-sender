package db

import (
	"strings"
	"testing"
)

func TestReadingsQuery(t *testing.T) {
	q, err := readingsQuery("SEL-751-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT TIME, VALUE FROM `SEL-751-1` WHERE TYPE = ? AND TIME >= ? AND TIME < ? ORDER BY TIME ASC"
	if q != want {
		t.Errorf("query = %q, quero %q", q, want)
	}
}

func TestReadingsQueryRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", "a`b", "x; DROP TABLE y", "a b", "LOG_SENSOR.x", "tab`; --"} {
		q, err := readingsQuery(name)
		if err == nil {
			t.Errorf("readingsQuery(%q) aceitou: %q", name, q)
		} else if !strings.Contains(err.Error(), "inválido") {
			t.Errorf("readingsQuery(%q) erro = %v", name, err)
		}
	}
}
