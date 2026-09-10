package gateway

import (
	"errors"
	"reflect"
	"testing"

	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

func TestSystemPACReportDistinguishesMissingAndEmptyObservations(t *testing.T) {
	failure := systempac.ObservationError{ServiceName: "VPN", Cause: errors.New("query failed")}
	report := systemPACReport(systempac.Observation{Services: []systempac.ServiceObservation{
		{Name: "VPN"},
		{Name: "Wi-Fi", State: &systempac.PACState{}},
	}}, "", failure)
	want := []SystemPACServiceState{
		{Name: "VPN"},
		{Name: "Wi-Fi", Manageable: true, Enabled: new(false)},
	}
	if !reflect.DeepEqual(report.Services, want) {
		t.Fatalf("services = %#v, want %#v", report.Services, want)
	}
	if len(report.Issues) != 1 || report.Issues[0].Kind != SystemPACIssueObservation || report.Issues[0].ServiceName != "VPN" || report.Issues[0].Cause != failure.Error() {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestSystemPACReportClassifiesNetworkServiceListFailure(t *testing.T) {
	failure := networkservice.ListError{Cause: errors.New("networksetup failed")}
	report := systemPACReport(systempac.Observation{}, "", failure)
	if len(report.Issues) != 1 || report.Issues[0].Kind != SystemPACIssueDiscovery || report.Issues[0].Cause != failure.Error() {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestSystemPACReportClassifiesUnfulfilledDelivery(t *testing.T) {
	mismatch := systempac.DeliveryMismatchError{
		ServiceName: "Wi-Fi", PACURL: "http://127.0.0.1:8080/seamless-cors.pac?v=2",
		Observed: systempac.PACState{URL: "http://127.0.0.1:8080/seamless-cors.pac?v=1", Enabled: true},
	}
	for _, test := range []struct {
		err     error
		kind    SystemPACIssueKind
		service string
	}{
		{systempac.NoEligibleServicesError{}, SystemPACIssueDelivery, ""},
		{mismatch, SystemPACIssueVerification, "Wi-Fi"},
	} {
		report := systemPACReport(systempac.Observation{}, "", test.err)
		if len(report.Issues) != 1 || report.Issues[0] != (SystemPACIssue{Kind: test.kind, ServiceName: test.service, Cause: test.err.Error()}) {
			t.Fatalf("issues = %#v", report.Issues)
		}
	}
}
