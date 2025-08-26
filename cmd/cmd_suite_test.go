package cmd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rraymondgh/plugins/tests"
)

func TestCmd(t *testing.T) {
	tests.Init(t, false)
	t.Parallel()

	RegisterFailHandler(Fail)
	RunSpecs(t, "Cmd Suite")
}
