package service

import (
	"context"
	"errors"
	"strings"

	"agentbox/internal/agentbox/types"
)

func (s *Service) CreateAPIKey(ctx context.Context, auth types.AuthContext, name string) (types.APIKey, error) {
	return s.CreateAPIKeyWithPurposeAndScopes(ctx, auth, name, "custom", defaultAPIKeyScopes())
}

func (s *Service) CreateAPIKeyWithScopes(ctx context.Context, auth types.AuthContext, name string, scopes []string) (types.APIKey, error) {
	return s.CreateAPIKeyWithPurposeAndScopes(ctx, auth, name, "custom", scopes)
}

func (s *Service) CreateAPIKeyWithPurposeAndScopes(ctx context.Context, auth types.AuthContext, name string, purpose string, scopes []string) (types.APIKey, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.APIKey{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return types.APIKey{}, errors.New("API key name is required.")
	}
	secret, err := generateSecret()
	if err != nil {
		return types.APIKey{}, err
	}
	created, err := s.repo.CreateAPIKey(ctx, auth.UserID, name, normalizeCredentialPurpose(purpose), hashSecret(secret), tokenPrefix(secret), normalizeScopes(scopes))
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return types.APIKey{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An active credential already uses that label. Choose a distinct label or rotate that exact credential.", Err: err}
	}
	if err != nil {
		return types.APIKey{}, err
	}
	created.Key = secret
	return created, nil
}

func (s *Service) GetOnboardingState(ctx context.Context, auth types.AuthContext) (types.OnboardingState, error) {
	if err := requireBrowserSession(auth); err != nil {
		return types.OnboardingState{}, err
	}
	return s.repo.GetOnboardingState(ctx, auth.UserID)
}

func (s *Service) DismissOnboarding(ctx context.Context, auth types.AuthContext) (types.OnboardingState, error) {
	if err := requireBrowserSession(auth); err != nil {
		return types.OnboardingState{}, err
	}
	return s.repo.DismissOnboarding(ctx, auth.UserID)
}

func (s *Service) CreateOnboardingConnection(ctx context.Context, auth types.AuthContext, connector string, baseURL string, rotate bool) (OnboardingConnectionResult, error) {
	if err := requireBrowserSession(auth); err != nil {
		return OnboardingConnectionResult{}, err
	}
	connector, name, purpose, scopes, err := onboardingCredentialSpec(connector)
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return OnboardingConnectionResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "deployment base URL is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	setupBaseURL := ""
	if connector == "raycast" {
		setupBaseURL = baseURL
	}
	credential, state, err := s.repo.CreateOnboardingCredential(ctx, auth.UserID, connector, name, purpose, hashSecret(secret), tokenPrefix(secret), scopes, setupBaseURL, rotate)
	if errors.Is(err, types.ErrOnboardingCredentialExists) {
		return OnboardingConnectionResult{}, CodedError{Code: "ONBOARDING_CREDENTIAL_EXISTS", Message: "This connector already has an active credential. Choose rotate to replace it.", Err: err}
	}
	if errors.Is(err, types.ErrOnboardingCredentialNotFound) {
		return OnboardingConnectionResult{}, CodedError{Code: "ONBOARDING_CREDENTIAL_NOT_FOUND", Message: "This connector does not have an active credential to rotate. Reconnect it instead.", Err: err}
	}
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return OnboardingConnectionResult{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An unrelated active credential already uses this connector label. Rename or revoke that credential before reconnecting onboarding.", Err: err}
	}
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	credential.Key = secret
	result := OnboardingConnectionResult{Connector: connector, Credential: credential, State: state}
	switch connector {
	case "chatgpt":
		result.MCPURL, err = mcpURLWithSecret(baseURL, secret)
		result.Instructions = []string{
			"Open ChatGPT settings and go to Apps & Connectors, then Advanced settings.",
			"Create a custom remote MCP connector and paste the complete URL below.",
			"Choose no authentication. The credential is already carried in the URL query string.",
			"Save the connector, then ask ChatGPT to list your AgentBox threads as a connection test.",
			"After connector schema changes, refresh or recreate the connector and run Scan Tools when available.",
			"To test attachments, create a ChatGPT file artifact and ask it to attach the file you just created; do not supply a file ID, path, or URL.",
		}
	case "claude":
		result.MCPURL, err = mcpURLWithSecret(baseURL, secret)
		result.Instructions = []string{
			"Open Claude's connector or integrations settings and add a custom remote MCP server.",
			"Paste the complete URL below without adding a separate authorization header.",
			"Name the connector AgentBox and finish the connection flow.",
			"Ask Claude to list your AgentBox threads as a connection test.",
		}
	case "local":
		result.ProfileCommand = localProfileCommand(baseURL, secret, auth.UserID, credential.Name)
		result.SetupPrompt = localAgentSetupPrompt(baseURL, secret, auth.UserID, credential.Name)
		result.Instructions = []string{
			"Copy the generated prompt and paste it into one local coding-agent session on the machine you want to connect.",
			"The agent will install the public npm package, save an active local profile, and run agentbox list.",
			"Use a separate credential later for each additional machine.",
		}
	case "raycast":
		setup := raycastSetupMaterial(baseURL, secret, credential.ID, credential.Name)
		result.RaycastSetup = &setup
		result.Instructions = []string{
			"Clone or update the AgentBox repository on the Mac where Raycast is installed.",
			"Install the extension dependencies and run it in Raycast developer mode with the commands below.",
			"Enter the generated Agentbox URL and Agentbox API Key in the required extension preferences.",
			"Run Browse Threads in Raycast and confirm it shows only the threads currently accessible to your user.",
			"Create a separate Raycast credential for every additional Mac or local Raycast installation.",
		}
	}
	if err != nil {
		return OnboardingConnectionResult{}, err
	}
	return result, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, auth types.AuthContext) ([]types.APIKey, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(ctx, auth.UserID)
}

func (s *Service) ListAPIKeysPage(ctx context.Context, auth types.AuthContext, pageRequest types.PageRequest) (types.APIKeyPage, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.APIKeyPage{}, err
	}
	return s.repo.ListAPIKeysPage(ctx, auth.UserID, pageRequest)
}

type RaycastInstallationResult struct {
	Credential types.APIKey               `json:"credential"`
	Setup      types.RaycastSetupMaterial `json:"raycast_setup"`
}

func (s *Service) CreateRaycastInstallation(ctx context.Context, auth types.AuthContext, label string, baseURL string) (RaycastInstallationResult, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return RaycastInstallationResult{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "A distinct Raycast installation label is required."}
	}
	if len(label) > 120 {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "Raycast installation labels must be at most 120 characters."}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return RaycastInstallationResult{}, CodedError{Code: "INVALID_ARGUMENT", Message: "deployment base URL is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return RaycastInstallationResult{}, err
	}
	credential, err := s.repo.CreateRaycastAPIKey(ctx, auth.UserID, label, hashSecret(secret), tokenPrefix(secret), ConnectorAPIKeyScopes("raycast"), baseURL)
	if errors.Is(err, types.ErrCredentialLabelConflict) {
		return RaycastInstallationResult{}, CodedError{Code: "CREDENTIAL_LABEL_CONFLICT", Message: "An active credential already uses that label. Choose a distinct installation label or rotate that exact credential.", Err: err}
	}
	if err != nil {
		return RaycastInstallationResult{}, err
	}
	credential.Key = secret
	return RaycastInstallationResult{
		Credential: credential,
		Setup:      raycastSetupMaterial(baseURL, secret, credential.ID, credential.Name),
	}, nil
}

func (s *Service) RotateAPIKeyByID(ctx context.Context, auth types.AuthContext, keyID string, baseURL string) (types.APIKey, *types.RaycastSetupMaterial, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.APIKey{}, nil, err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return types.APIKey{}, nil, CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	secret, err := generateSecret()
	if err != nil {
		return types.APIKey{}, nil, err
	}
	rotated, setupBaseURL, err := s.repo.RotateAPIKeyForUserByID(ctx, auth.UserID, keyID, hashSecret(secret), tokenPrefix(secret), strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if errors.Is(err, types.ErrRaycastSetupUnavailable) {
		return types.APIKey{}, nil, CodedError{Code: "RAYCAST_SETUP_UNAVAILABLE", Message: "Raycast setup metadata is unavailable for this credential, so its secret was not rotated.", Err: err}
	}
	if err != nil {
		return types.APIKey{}, nil, err
	}
	if rotated == nil {
		return types.APIKey{}, nil, CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	rotated.Key = secret
	var setup *types.RaycastSetupMaterial
	if rotated.Purpose == "raycast" {
		material := raycastSetupMaterial(setupBaseURL, secret, rotated.ID, rotated.Name)
		setup = &material
	}
	return *rotated, setup, nil
}

func (s *Service) RevokeAPIKeyByID(ctx context.Context, auth types.AuthContext, keyID string) error {
	if err := requireUserAuthContext(auth); err != nil {
		return err
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return CodedError{Code: "INVALID_ARGUMENT", Message: "credential_id is required."}
	}
	revoked, err := s.repo.RevokeAPIKeyForUserByID(ctx, auth.UserID, keyID)
	if err != nil {
		return err
	}
	if !revoked {
		return CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Credential not found.", Err: ErrAPIKeyNotFound}
	}
	return nil
}

func (s *Service) RaycastInstallationSetup(ctx context.Context, auth types.AuthContext, keyID string, baseURL string) (types.RaycastSetupMaterial, error) {
	if err := requireUserAuthContext(auth); err != nil {
		return types.RaycastSetupMaterial{}, err
	}
	key, setupBaseURL, err := s.repo.GetAPIKeySetup(ctx, auth.UserID, strings.TrimSpace(keyID), strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil {
		return types.RaycastSetupMaterial{}, err
	}
	if key == nil || key.Purpose != "raycast" {
		return types.RaycastSetupMaterial{}, CodedError{Code: "CREDENTIAL_NOT_FOUND", Message: "Raycast installation credential not found.", Err: ErrAPIKeyNotFound}
	}
	if strings.TrimSpace(setupBaseURL) == "" {
		return types.RaycastSetupMaterial{}, CodedError{Code: "RAYCAST_SETUP_UNAVAILABLE", Message: "Raycast setup metadata is unavailable for this credential."}
	}
	return raycastSetupMaterial(setupBaseURL, "", key.ID, key.Name), nil
}
