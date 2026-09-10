package gateway

import (
	"github.com/QzCurious/seamless-cors/internal/lib/networkservice"
	"github.com/QzCurious/seamless-cors/internal/systempac"
)

func openSystemPAC() systempac.Module { return systempac.New() }

func systemPACReport(state systempac.Observation, endpoint string, err error) SystemPACReport {
	report := SystemPACReport{
		Services:              make([]SystemPACServiceState, 0, len(state.Services)),
		RoutesCurrentEndpoint: state.RoutesEndpoint(endpoint),
	}
	for _, service := range state.Services {
		if service.State == nil {
			report.Services = append(report.Services, SystemPACServiceState{Name: service.Name})
			continue
		}
		report.Services = append(report.Services, SystemPACServiceState{
			Name: service.Name, URL: service.State.URL, Enabled: &service.State.Enabled,
			Manageable: service.State.Manageable(), Owned: service.State.Owned(),
		})
	}
	walkErrors(err, func(item error) {
		issue := SystemPACIssue{Cause: item.Error()}
		switch typed := item.(type) {
		case networkservice.ListError:
			issue.Kind = SystemPACIssueDiscovery
		case systempac.ObservationError:
			issue.Kind, issue.ServiceName = SystemPACIssueObservation, typed.ServiceName
		case systempac.MutationError:
			issue.Kind, issue.ServiceName = SystemPACIssueMutation, typed.ServiceName
		case systempac.VerificationError:
			issue.Kind, issue.ServiceName = SystemPACIssueVerification, typed.ServiceName
		case systempac.NoEligibleServicesError:
			issue.Kind = SystemPACIssueDelivery
		case systempac.DeliveryMismatchError:
			issue.Kind, issue.ServiceName = SystemPACIssueVerification, typed.ServiceName
		case systempac.ResidueError:
			issue.Kind = SystemPACIssueResidue
		default:
			return
		}
		report.Issues = append(report.Issues, issue)
	})
	return report
}

func walkErrors(err error, visit func(error)) {
	if err == nil {
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			walkErrors(child, visit)
		}
		return
	}
	visit(err)
}
