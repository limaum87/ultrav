package libvirt

import (
	"encoding/xml"
	"testing"
)

// Regression: the memory/currentMemory element text must be read via the
// `,chardata` tag. `chardata` (without the comma) makes encoding/xml look for
// a child element named <chardata>, silently yielding 0 and leaving the VM's
// allocated memory at 0 B in the API.
func TestDomainXMLMemoryParsing(t *testing.T) {
	const doc = `<domain>
  <name>web01</name>
  <memory unit='KiB'>2097152</memory>
  <currentMemory unit='KiB'>2097152</currentMemory>
  <vcpu>2</vcpu>
</domain>`

	var dx domainXML
	if err := xml.Unmarshal([]byte(doc), &dx); err != nil {
		t.Fatalf("unmarshal domain XML: %v", err)
	}
	if dx.Memory.Value == 0 {
		t.Fatal("memory value decoded as 0 (chardata tag broken)")
	}
	if got, want := normalizeBytes(dx.Memory.Value, dx.Memory.Unit), int64(2)*1024*1024*1024; got != want {
		t.Fatalf("memory = %d bytes, want %d", got, want)
	}
	if got, want := normalizeBytes(dx.CurrentMemory.Value, dx.CurrentMemory.Unit), int64(2)*1024*1024*1024; got != want {
		t.Fatalf("currentMemory = %d bytes, want %d", got, want)
	}
	if dx.VCPU != 2 {
		t.Fatalf("vcpu = %d, want 2", dx.VCPU)
	}
}

func TestNormalizeBytes(t *testing.T) {
	cases := []struct {
		value int64
		unit  string
		want  int64
	}{
		{1024, "KiB", 1024 * 1024},
		{512, "MiB", 512 * 1024 * 1024},
		{1, "GiB", 1024 * 1024 * 1024},
		{2048, "", 2048},
		{2, "MB", 2 * 1000 * 1000},
	}
	for _, c := range cases {
		if got := normalizeBytes(c.value, c.unit); got != c.want {
			t.Errorf("normalizeBytes(%d, %q) = %d, want %d", c.value, c.unit, got, c.want)
		}
	}
}
