package gateway

import "time"

// StartKind identifies the semantic outcome of a Start operation.  Start
// outcomes are deliberately scoped to Start; they are not shared with the
// other Gateway commands.
type StartKind string

type CommandFulfillment string

const (
	CommandFulfilled   CommandFulfillment = "fulfilled"
	CommandUnfulfilled CommandFulfillment = "unfulfilled"
)

const (
	StartResultStarted                             StartKind = "started"
	StartResultAlreadyRunning                      StartKind = "already-running"
	StartResultOwnerTransition                     StartKind = "owner-transition"
	StartResultUpstreamListCreationConsentRequired StartKind = "upstream-list-creation-consent-required"
	StartResultStartAlreadyMutating                StartKind = "start-already-mutating"
	StartResultStopCancelled                       StartKind = "stop-cancelled"
	StartResultCleanupFailed                       StartKind = "cleanup-failed"
)

type StartRequest struct {
	WorkingDirectory            string                            `json:"workingDirectory" minLength:"1"`
	UpstreamListCreationConsent *UpstreamListCreationConsentInput `json:"upstreamListCreationConsent,omitempty"`
}

// StartResult is the closed semantic result of a Start operation.  Concrete
// variants carry only the payload that is legal for that outcome.
type StartResult interface {
	Kind() StartKind
	Fulfillment() CommandFulfillment
	UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail
	startResult()
}

// Started reports that the runtime was started and includes initial guidance.
type Started struct {
	Guidance                    StartGuidance
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// AlreadyRunning reports that a runtime was already active.
type AlreadyRunning struct{}

// StartOwnerTransition reports that ownership is being acquired or released.
type StartOwnerTransition struct{}

type StartUpstreamListCreationConsentRequired struct{ Consent UpstreamListCreationConsent }

type UpstreamListCreationConsent struct {
	Path                     string                          `json:"path"`
	DefaultContents          string                          `json:"defaultContents"`
	MissingParentDirectories []string                        `json:"missingParentDirectories,omitempty"`
	Fingerprint              UpstreamListCreationFingerprint `json:"fingerprint"`
}

type UpstreamListCreationDecision string

const (
	UpstreamListCreationAccepted UpstreamListCreationDecision = "accepted"
	UpstreamListCreationDeclined UpstreamListCreationDecision = "declined"
)

type UpstreamListCreationFingerprint string
type UpstreamListCreationConsentInput struct {
	Decision    UpstreamListCreationDecision    `json:"decision"`
	Fingerprint UpstreamListCreationFingerprint `json:"fingerprint,omitempty"`
}

// StartAlreadyMutating reports that another start/CA mutation is in progress.
type StartAlreadyMutating struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

// StartStopCancelled reports that Stop cancelled the Start operation.
type StartStopCancelled struct {
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

type StartCleanupFailed struct {
	Failures                    []CleanupFailure
	UpstreamListCreationWarning *UpstreamListCreationWarningDetail
}

type UpstreamListCreationWarningDetail struct {
	Cause string `json:"cause"`
}

func startFulfillment(kind StartKind) CommandFulfillment {
	if kind == StartResultStarted || kind == StartResultAlreadyRunning {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

func (Started) Kind() StartKind                 { return StartResultStarted }
func (Started) Fulfillment() CommandFulfillment { return startFulfillment(StartResultStarted) }
func (r Started) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (Started) startResult()           {}
func (AlreadyRunning) Kind() StartKind { return StartResultAlreadyRunning }
func (AlreadyRunning) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultAlreadyRunning)
}
func (AlreadyRunning) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (AlreadyRunning) startResult()          {}
func (StartOwnerTransition) Kind() StartKind { return StartResultOwnerTransition }
func (StartOwnerTransition) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultOwnerTransition)
}
func (StartOwnerTransition) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (StartOwnerTransition) startResult() {}
func (StartUpstreamListCreationConsentRequired) Kind() StartKind {
	return StartResultUpstreamListCreationConsentRequired
}
func (StartUpstreamListCreationConsentRequired) Fulfillment() CommandFulfillment {
	return CommandUnfulfilled
}
func (StartUpstreamListCreationConsentRequired) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return nil
}
func (StartUpstreamListCreationConsentRequired) startResult() {}
func (StartAlreadyMutating) Kind() StartKind                  { return StartResultStartAlreadyMutating }
func (StartAlreadyMutating) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultStartAlreadyMutating)
}
func (r StartAlreadyMutating) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartAlreadyMutating) startResult()  {}
func (StartStopCancelled) Kind() StartKind { return StartResultStopCancelled }
func (StartStopCancelled) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultStopCancelled)
}
func (r StartStopCancelled) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartStopCancelled) startResult()    {}
func (StartCleanupFailed) Kind() StartKind { return StartResultCleanupFailed }
func (StartCleanupFailed) Fulfillment() CommandFulfillment {
	return startFulfillment(StartResultCleanupFailed)
}
func (r StartCleanupFailed) UpstreamListCreationWarningDetail() *UpstreamListCreationWarningDetail {
	return r.UpstreamListCreationWarning
}
func (StartCleanupFailed) startResult() {}

type SystemPACServiceState struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Enabled    *bool  `json:"enabled,omitempty"` // Nil when the setting could not be observed.
	Manageable bool   `json:"manageable"`
	Owned      bool   `json:"owned"`
}

type SystemPACIssueKind string

const (
	SystemPACIssueDelivery     SystemPACIssueKind = "delivery"
	SystemPACIssueDiscovery    SystemPACIssueKind = "discovery"
	SystemPACIssueObservation  SystemPACIssueKind = "observation"
	SystemPACIssueMutation     SystemPACIssueKind = "mutation"
	SystemPACIssueVerification SystemPACIssueKind = "verification"
	SystemPACIssueResidue      SystemPACIssueKind = "residue"
)

type SystemPACIssue struct {
	Kind        SystemPACIssueKind `json:"kind"`
	ServiceName string             `json:"serviceName,omitempty"`
	Cause       string             `json:"cause"`
}

type SystemPACReport struct {
	Services              []SystemPACServiceState `json:"services"`
	RoutesCurrentEndpoint bool                    `json:"routesCurrentEndpoint"`
	Issues                []SystemPACIssue        `json:"issues,omitempty"`
}

type StartGuidance struct {
	UpstreamLists []UpstreamListSourceDetail `json:"upstreamLists"`
	SystemPAC     SystemPACReport            `json:"systemPac"`
	Traffic       TrafficStatusDetail        `json:"traffic"`
	InstalledCA   InstalledCAStatusDetail    `json:"installedCA"`
	UserCAIssue   *UserCAAssessmentIssue     `json:"userCAAssessmentIssue,omitempty"`
}

type UpstreamListSourceKind string

const (
	UpstreamListSourceGlobal    UpstreamListSourceKind = "global"
	UpstreamListSourceDirectory UpstreamListSourceKind = "directory"
)

type UpstreamListSourceDetail struct {
	Kind            UpstreamListSourceKind       `json:"kind"`
	Path            string                       `json:"path"`
	Warnings        []UpstreamListWarningDetail  `json:"warnings,omitempty"`
	FileSyncIssue   *FileSyncIssue               `json:"fileSyncIssue,omitempty"`
	ProjectionIssue *UpstreamListProjectionIssue `json:"projectionIssue,omitempty"`
}

type UpstreamListWarningDetail struct {
	Source     UpstreamListSourceKind `json:"source"`
	Path       string                 `json:"path"`
	Line       int                    `json:"line"`
	Text       string                 `json:"text"`
	Diagnostic string                 `json:"diagnostic"`
}

type FileSyncIssueKind string

const (
	FileSyncIssueFileUnreadable     FileSyncIssueKind = "file-unreadable"
	FileSyncIssueObservationStopped FileSyncIssueKind = "observation-stopped"
)

type FileSyncIssue struct {
	Kind  FileSyncIssueKind `json:"kind"`
	Cause string            `json:"cause"`
}

type UpstreamListProjectionIssue struct {
	Cause string `json:"cause"`
}

type StopResultKind string

const (
	StopResultStopped    StopResultKind = "stopped"
	StopResultNotRunning StopResultKind = "not-running"
)

type StopResult struct {
	Kind               StopResultKind
	Warnings           []CommandWarning
	CleanupFulfillment CommandFulfillment
	SystemPACCleanup   SystemPACReport
	CleanupFailures    []CleanupFailure
}

func (r StopResult) Fulfillment() CommandFulfillment {
	return CommandFulfilled
}

type CleanupFailure struct {
	Subject    CleanupSubjectKind `json:"subject"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CleanupSubjectKind string

const (
	CleanupSubjectSystemPAC         CleanupSubjectKind = "system-pac"
	CleanupSubjectGatewayStateCache CleanupSubjectKind = "gateway-state-cache"
)

type CommandWarning struct {
	Kind       CommandWarningKind `json:"kind"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type CommandWarningKind string

const (
	CommandWarningRuntimeCloseFailed CommandWarningKind = "runtime-close-failed"
)

type InstallResultKind string

const (
	InstallResultInstalled       InstallResultKind = "installed"
	InstallResultAlreadyMutating InstallResultKind = "already-mutating"
	InstallResultOwnerEnding     InstallResultKind = "owner-ending"
	InstallResultOwnerTransition InstallResultKind = "owner-transition"
)

type InstallResult struct {
	Kind               InstallResultKind
	InstalledCAExpires time.Time
}

func (r InstallResult) Fulfillment() CommandFulfillment {
	if r.Kind == InstallResultInstalled {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type UninstallResultKind string

const (
	UninstallResultUninstalled     UninstallResultKind = "uninstalled"
	UninstallResultConsentRequired UninstallResultKind = "consent-required"
	UninstallResultAlreadyMutating UninstallResultKind = "already-mutating"
	UninstallResultOwnerEnding     UninstallResultKind = "owner-ending"
	UninstallResultOwnerTransition UninstallResultKind = "owner-transition"
	UninstallResultIncomplete      UninstallResultKind = "incomplete"
)

type UninstallResult struct {
	Kind               UninstallResultKind
	ConsentFingerprint string
	CleanupIssue       *UserCACleanupIssue
}

func (r UninstallResult) Fulfillment() CommandFulfillment {
	if r.Kind == UninstallResultUninstalled {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type UninstallRequest struct {
	ConsentFingerprint string `json:"consentFingerprint,omitempty"`
}

type GatewayStatusKind string

type StatusResultKind string

const (
	StatusResultReported        StatusResultKind = "reported"
	StatusResultOwnerTransition StatusResultKind = "owner-transition"
)

const (
	GatewayStatusNotRunning GatewayStatusKind = "not-running"
	GatewayStatusStaleCache GatewayStatusKind = "stale-cache"
	GatewayStatusRouterOnly GatewayStatusKind = "router-only"
	GatewayStatusEnding     GatewayStatusKind = "ending"
	GatewayStatusStarting   GatewayStatusKind = "starting"
	GatewayStatusRunning    GatewayStatusKind = "running"
)

type StatusResult struct {
	Kind StatusResultKind
	StatusReport
}

type StatusReport struct {
	State                 GatewayStatusKind       `json:"state"`
	Owner                 *OwnerStatusDetail      `json:"owner,omitempty"`
	Runtime               *RuntimeStatusDetail    `json:"runtime,omitempty"`
	SystemPAC             SystemPACReport         `json:"systemPac"`
	Cleanup               CleanupStatusDetail     `json:"cleanup"`
	InstalledCA           InstalledCAStatusDetail `json:"installedCA"`
	UserCAAssessmentIssue *UserCAAssessmentIssue  `json:"userCAAssessmentIssue,omitempty"`
}

func (r StatusResult) Fulfillment() CommandFulfillment {
	if r.Kind == StatusResultReported {
		return CommandFulfilled
	}
	return CommandUnfulfilled
}

type OwnerStatusDetail struct {
	RouterListen string `json:"routerListen"`
}

type RuntimeStatusDetail struct {
	ProxyListen             string                     `json:"proxyListen"`
	PACListen               string                     `json:"pacListen"`
	UpstreamLists           []UpstreamListSourceDetail `json:"upstreamLists"`
	UpstreamCount           int                        `json:"upstreamCount"`
	Traffic                 TrafficStatusDetail        `json:"traffic"`
	LatestSystemPACDelivery *SystemPACReport           `json:"latestSystemPacDelivery,omitempty"`
}

type TrafficFeatureState string

const (
	TrafficFeatureActive   TrafficFeatureState = "active"
	TrafficFeatureBlocked  TrafficFeatureState = "blocked"
	TrafficFeatureInactive TrafficFeatureState = "inactive"
)

type TrafficStatusDetail struct {
	RoutingReady bool                `json:"routingReady"`
	HTTPCORS     TrafficFeatureState `json:"httpCors"`
	HTTPSCORS    TrafficFeatureState `json:"httpsCors"`
	HTTPSFacade  TrafficFeatureState `json:"httpsFacade"`
}

type UserCAAssessmentIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
}

type UserCACleanupIssue struct {
	Cause  string `json:"cause"`
	Action string `json:"action,omitempty"`
}

type CleanupStatusDetail struct {
	State    CleanupStatusState           `json:"state"`
	Subjects []CleanupSubjectStatusDetail `json:"subjects"`
}

type CleanupStatusState string

const (
	CleanupStatusNone    CleanupStatusState = "none"
	CleanupStatusNeeded  CleanupStatusState = "needed"
	CleanupStatusUnknown CleanupStatusState = "unknown"
)

type CleanupSubjectStatusDetail struct {
	Subject    CleanupSubjectKind `json:"subject"`
	State      CleanupStatusState `json:"state"`
	Diagnostic string             `json:"diagnostic,omitempty"`
}

type InstalledCAStatusDetail struct {
	Health       CAHealthStatus      `json:"health"`
	Expires      time.Time           `json:"expires,omitempty"`
	RenewalDue   bool                `json:"renewalDue,omitempty"`
	CleanupIssue *UserCACleanupIssue `json:"cleanupIssue,omitempty"`
}

type CAHealthStatus string

const (
	CAHealthUsable    CAHealthStatus = "usable"
	CAHealthNotUsable CAHealthStatus = "not-usable"
	CAHealthMutating  CAHealthStatus = "mutating"
)
