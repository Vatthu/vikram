from __future__ import annotations

import httpx

from .models import (
    ArtifactReadRequest,
    ArtifactReadResponse,
    ArtifactWriteRequest,
    ArtifactWriteResponse,
    ChannelNotificationRequest,
    ChannelNotificationResponse,
    FileReadRequest,
    FileReadResponse,
    FileReplaceRequest,
    FileReplaceResponse,
    FileWriteRequest,
    FileWriteResponse,
    GitRollbackRequest,
    GitRollbackResponse,
    GitWorktreeCreateRequest,
    GitWorktreeCreateResponse,
    GitWorktreeRemoveRequest,
    GitWorktreeRemoveResponse,
    HostActionRequest,
    HostObservation,
    AgentThinkRequest,
    AgentThinkResponse,
    AgentRosterResponse,
    ChangeReviewRequest,
    ChangeReviewResponse,
    LintDiscoveryRequest,
    LintDiscoveryResponse,
    LintRunRequest,
    LintRunResponse,
    RepoInspectRequest,
    RepoInspectResponse,
    RepoTargetDiscoveryRequest,
    RepoTargetDiscoveryResponse,
    SystemHealthResponse,
    VerificationDiscoveryRequest,
    VerificationDiscoveryResponse,
    WorkspaceProvisionRequest,
    WorkspaceProvisionResponse,
    BrowserTestRequest,
    BrowserTestResponse,
)


class HostClient:
    def __init__(self, socket_path: str) -> None:
        self.socket_path = socket_path
        transport = httpx.HTTPTransport(uds=socket_path)
        self._client = httpx.Client(transport=transport, base_url="http://vikramd")

    def _raise_for_status(self, response: httpx.Response) -> None:
        try:
            response.raise_for_status()
        except httpx.HTTPStatusError as exc:
            detail = response.text.strip()
            if detail:
                raise httpx.HTTPStatusError(
                    f"{exc}. Response body: {detail}",
                    request=exc.request,
                    response=exc.response,
                ) from exc
            raise

    def _get(self, path: str) -> httpx.Response:
        response = self._client.get(path)
        self._raise_for_status(response)
        return response

    def _post(self, path: str, payload: dict[str, object]) -> httpx.Response:
        response = self._client.post(path, json=payload)
        self._raise_for_status(response)
        return response

    def health(self) -> SystemHealthResponse:
        response = self._get("/v1/system/health")
        return SystemHealthResponse.model_validate(response.json())

    def provision_workspace(
        self, request: WorkspaceProvisionRequest
    ) -> WorkspaceProvisionResponse:
        response = self._post("/v1/workspaces/provision", request.model_dump())
        return WorkspaceProvisionResponse.model_validate(response.json())

    def create_worktree(
        self, request: GitWorktreeCreateRequest
    ) -> GitWorktreeCreateResponse:
        response = self._post("/v1/git/worktrees/create", request.model_dump())
        return GitWorktreeCreateResponse.model_validate(response.json())

    def remove_worktree(
        self, request: GitWorktreeRemoveRequest
    ) -> GitWorktreeRemoveResponse:
        response = self._post("/v1/git/worktrees/remove", request.model_dump())
        return GitWorktreeRemoveResponse.model_validate(response.json())

    def inspect_repo(self, request: RepoInspectRequest) -> RepoInspectResponse:
        response = self._post("/v1/repos/inspect", request.model_dump())
        return RepoInspectResponse.model_validate(response.json())

    def discover_targets(
        self, request: RepoTargetDiscoveryRequest
    ) -> RepoTargetDiscoveryResponse:
        response = self._post("/v1/repos/discover-targets", request.model_dump())
        return RepoTargetDiscoveryResponse.model_validate(response.json())

    def read_file(self, request: FileReadRequest) -> FileReadResponse:
        response = self._post("/v1/files/read", request.model_dump())
        return FileReadResponse.model_validate(response.json())

    def write_file(self, request: FileWriteRequest) -> FileWriteResponse:
        response = self._post("/v1/files/write", request.model_dump())
        return FileWriteResponse.model_validate(response.json())

    def replace_in_file(self, request: FileReplaceRequest) -> FileReplaceResponse:
        response = self._post("/v1/files/replace", request.model_dump())
        return FileReplaceResponse.model_validate(response.json())

    def discover_verification(
        self, request: VerificationDiscoveryRequest
    ) -> VerificationDiscoveryResponse:
        response = self._post("/v1/repos/discover-verification", request.model_dump())
        return VerificationDiscoveryResponse.model_validate(response.json())

    def write_artifact(
        self, request: ArtifactWriteRequest
    ) -> ArtifactWriteResponse:
        response = self._post("/v1/artifacts/write", request.model_dump())
        return ArtifactWriteResponse.model_validate(response.json())

    def read_artifact(self, request: ArtifactReadRequest) -> ArtifactReadResponse:
        response = self._post("/v1/artifacts/read", request.model_dump())
        return ArtifactReadResponse.model_validate(response.json())

    def exec(self, request: HostActionRequest) -> HostObservation:
        response = self._post("/v1/exec", request.model_dump())
        return HostObservation.model_validate(response.json())

    def notify_telegram(
        self, request: ChannelNotificationRequest
    ) -> ChannelNotificationResponse:
        response = self._post("/v1/notify/telegram", request.model_dump())
        return ChannelNotificationResponse.model_validate(response.json())

    def rollback_worktree(
        self, request: GitRollbackRequest
    ) -> GitRollbackResponse:
        response = self._post("/v1/git/rollback", request.model_dump())
        return GitRollbackResponse.model_validate(response.json())

    def discover_lint(
        self, request: LintDiscoveryRequest
    ) -> LintDiscoveryResponse:
        response = self._post("/v1/repos/discover-lint", request.model_dump())
        return LintDiscoveryResponse.model_validate(response.json())

    def run_lint(self, request: LintRunRequest) -> LintRunResponse:
        response = self._post("/v1/repos/run-lint", request.model_dump())
        return LintRunResponse.model_validate(response.json())

    def review_change(
        self, request: ChangeReviewRequest
    ) -> ChangeReviewResponse:
        response = self._post("/v1/review/change", request.model_dump())
        return ChangeReviewResponse.model_validate(response.json())

    def browser_test(self, request: BrowserTestRequest) -> BrowserTestResponse:
        response = self._post("/v1/browser/test", request.model_dump())
        return BrowserTestResponse.model_validate(response.json())

    def agent_roster(self) -> AgentRosterResponse:
        response = self._get("/v1/agent/roster")
        return AgentRosterResponse.model_validate(response.json())

    def agent_think(
        self, request: AgentThinkRequest
    ) -> AgentThinkResponse:
        response = self._post("/v1/agent/think", request.model_dump())
        return AgentThinkResponse.model_validate(response.json())

    def close(self) -> None:
        self._client.close()
