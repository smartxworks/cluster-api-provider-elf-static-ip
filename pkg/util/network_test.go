package util

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestPing(t *testing.T) {
	if SkipPingIP() {
		return
	}

	g := NewWithT(t)

	reachable, err := Ping("127.0.0.1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(reachable).To(BeTrue())

	reachable, err = Ping("10.123.1.1")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(reachable).To(BeFalse())
}
