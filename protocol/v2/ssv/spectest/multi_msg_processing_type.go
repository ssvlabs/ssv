package spectest

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
)

type MultiMsgProcessingSpecTest struct {
	Name  string
	Tests []*MsgProcessingSpecTest

	logger *zap.Logger
}

func (tests *MultiMsgProcessingSpecTest) TestName() string {
	return tests.Name
}

func (tests *MultiMsgProcessingSpecTest) Run(t *testing.T) {
	tests.logger = logging.TestLogger(t)
	tests.overrideStateComparison(t)

	for _, test := range tests.Tests {
		t.Run(test.TestName(), func(t *testing.T) {
			test.ParentName = tests.Name
			test.RunAsPartOfMultiTest(t, tests.logger)
		})
	}
}

// overrideStateComparison overrides the post state comparison for all tests in the multi test
func (tests *MultiMsgProcessingSpecTest) overrideStateComparison(t *testing.T) {
	testsName := strings.ReplaceAll(tests.TestName(), " ", "_")
	for _, test := range tests.Tests {
		path := filepath.Join(testsName, test.TestName())
		testType := reflect.TypeOf(tests).String()
		testType = strings.Replace(testType, "spectest.", "tests.", 1)
		overrideStateComparison(t, test, path, testType)
	}
}
