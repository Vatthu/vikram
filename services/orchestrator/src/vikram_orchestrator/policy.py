from __future__ import annotations

from pathlib import PurePosixPath
from typing import Mapping

from .models import ApprovalPolicyDecision


def decide_approval_policy(state: Mapping[str, object]) -> ApprovalPolicyDecision:
    risk_class = "low"
    reasons: list[str] = []

    verification_outcome = str(state.get("verification_outcome", "")).strip()
    lint_passed = bool(state.get("lint_passed", True))
    review_verdict = str(state.get("review_verdict", "APPROVE")).strip()
    applied_paths = normalized_applied_paths(state)
    require_human_approval = bool(state.get("require_human_approval", False))
    active_follow_up_phase = str(state.get("active_follow_up_phase", "")).strip()

    # Lint guard: new lint errors after edits are a hard block.
    if not lint_passed:
        lint_errors = state.get("lint_new_errors", [])
        reasons.append(f"lint check failed with {len(lint_errors)} new error(s)")
        route = "founder_review" if require_human_approval else "stop"
        summary = (
            f"Lint check found {len(lint_errors)} new error(s); founder review required"
            if route == "founder_review"
            else "Lint check failed and founder approval is disabled"
        )
        return ApprovalPolicyDecision(
            risk_class="critical",
            route=route,
            reasons=reasons,
            summary=summary,
            options=approval_options_for_route(route, verification_outcome),
        )

    # LLM judge verdict: independent model review overrides auto-complete.
    if review_verdict == "REJECT":
        reasons.append("independent reviewer rejected the change")
        route = "stop"
        return ApprovalPolicyDecision(
            risk_class="critical",
            route=route,
            reasons=reasons,
            summary="Change rejected by independent reviewer; task cannot proceed",
            options=[],
        )
    if review_verdict == "CHANGES_REQUESTED":
        reasons.append("independent reviewer requested changes")
        route = "founder_review" if require_human_approval else "stop"
        summary = (
            "Independent reviewer requested changes; founder review required"
            if route == "founder_review"
            else "Independent reviewer requested changes and founder approval is disabled"
        )
        return ApprovalPolicyDecision(
            risk_class="high",
            route=route,
            reasons=reasons,
            summary=summary,
            options=approval_options_for_route(route, verification_outcome),
        )

    if verification_outcome == "failed":
        reasons.append("focused verification failed")
        route = "founder_review" if require_human_approval else "stop"
        summary = (
            "Founder review required because focused verification failed"
            if route == "founder_review"
            else "Verification failed and founder approval is disabled for this task"
        )
        return ApprovalPolicyDecision(
            risk_class="critical",
            route=route,
            reasons=reasons,
            summary=summary,
            options=approval_options_for_route(route, verification_outcome),
        )

    if not applied_paths:
        reasons.append("no applied edits were recorded")
        route = "founder_review" if require_human_approval else "stop"
        summary = (
            "Founder review required because no applied edits were recorded"
            if route == "founder_review"
            else "No applied edits were recorded and founder approval is disabled for this task"
        )
        return ApprovalPolicyDecision(
            risk_class="medium",
            route=route,
            reasons=reasons,
            summary=summary,
            options=approval_options_for_route(route, verification_outcome),
        )

    if active_follow_up_phase in {
        "founder_edit_requested",
        "founder_clarification_requested",
        "merge_blocked",
    }:
        reasons.append(
            f"task is resuming from prior follow-up phase `{active_follow_up_phase}`"
        )
        return ApprovalPolicyDecision(
            risk_class=max_risk(risk_class, "medium"),
            route="founder_review",
            reasons=reasons,
            summary=(
                "Founder review required because the task is resuming from prior follow-up "
                f"phase `{active_follow_up_phase}`"
            ),
            options=approval_options_for_route("founder_review", verification_outcome),
        )

    if len(applied_paths) > 1:
        risk_class = max_risk(risk_class, "medium")
        reasons.append("multiple files were changed in one attempt")

    if any(is_configuration_or_automation_path(path) for path in applied_paths):
        risk_class = max_risk(risk_class, "high")
        reasons.append("configuration or automation files were changed")
    elif any(not looks_documentation_path(path) for path in applied_paths):
        risk_class = max_risk(risk_class, "high")
        reasons.append("code files were changed")

    if risk_class == "low":
        return ApprovalPolicyDecision(
            risk_class="low",
            route="auto_complete",
            reasons=[],
            summary="Low-risk documentation change verified successfully and auto-completed",
            options=[],
        )

    route = "founder_review" if require_human_approval else "auto_complete"
    reason_summary = "; ".join(reasons) or "policy escalation"
    if route == "founder_review":
        summary = "Founder review required because " + reason_summary
    else:
        summary = (
            "High-risk change verified successfully and auto-completed because "
            f"founder approval is disabled for this task: {reason_summary}"
        )

    return ApprovalPolicyDecision(
        risk_class=risk_class,
        route=route,
        reasons=reasons,
        summary=summary,
        options=approval_options_for_route(route, verification_outcome),
    )


def approval_options_for_route(route: str, verification_outcome: str) -> list[str]:
    if route != "founder_review":
        return []
    options = ["approve", "reject", "clarify"]
    if verification_outcome != "failed":
        options.insert(1, "edit_and_approve")
    return options


def normalized_applied_paths(state: Mapping[str, object]) -> list[str]:
    paths: list[str] = []
    for item in state.get("applied_edits", []):
        if not isinstance(item, Mapping):
            continue
        path = str(item.get("path", "")).strip()
        if path:
            paths.append(path)
    return paths


def max_risk(left: str, right: str) -> str:
    ordering = {"low": 0, "medium": 1, "high": 2, "critical": 3}
    return left if ordering[left] >= ordering[right] else right


def looks_documentation_path(path: str) -> bool:
    normalized = path.strip().lower()
    if normalized.endswith((".md", ".rst", ".txt", ".adoc")):
        return True
    return normalized.startswith(("docs/", "doc/", "documentation/"))


def is_configuration_or_automation_path(path: str) -> bool:
    normalized = path.strip().lower()
    parts = PurePosixPath(normalized).parts
    if not parts:
        return False

    filename = parts[-1]
    if filename in {
        "go.mod",
        "go.sum",
        "package.json",
        "package-lock.json",
        "pnpm-lock.yaml",
        "yarn.lock",
        "pyproject.toml",
        "poetry.lock",
        "requirements.txt",
        "dockerfile",
        "docker-compose.yml",
        "docker-compose.yaml",
        "makefile",
    }:
        return True

    if filename.endswith(
        (".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env")
    ):
        return True

    return normalized.startswith(
        (
            ".github/",
            ".gitlab/",
            ".circleci/",
            "ci/",
            "infra/",
            "ops/",
            "deploy/",
            "k8s/",
            "helm/",
            "terraform/",
            "scripts/",
        )
    )
