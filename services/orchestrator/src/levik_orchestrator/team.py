from __future__ import annotations

from collections.abc import Iterable

from .models import AgentProfile, AgentThinkRequest


class TeamRouter:
    """Selects an available model route from host-provided team metadata."""

    def __init__(self, agents: Iterable[AgentProfile]) -> None:
        self._agents = list(agents)

    @classmethod
    def from_state(cls, roster: object) -> TeamRouter:
        agents: list[AgentProfile] = []
        if isinstance(roster, list):
            for item in roster:
                try:
                    agents.append(AgentProfile.model_validate(item))
                except Exception:
                    continue
        return cls(agents)

    def request(self, task_id: str, role: str, prompt: str) -> AgentThinkRequest:
        selected = self._select(role)
        if selected is None:
            return AgentThinkRequest(task_id=task_id, role=role, prompt=prompt)

        return AgentThinkRequest(
            task_id=task_id,
            role=selected.role or role,
            prompt=prompt,
            provider=selected.provider,
            model=selected.model,
        )

    def _select(self, role: str) -> AgentProfile | None:
        normalized = role.strip().lower()
        if not normalized:
            return None

        for agent in self._agents:
            if agent.role.strip().lower() == normalized:
                return agent

        for agent in self._agents:
            capabilities = {cap.strip().lower() for cap in agent.capabilities}
            if normalized in capabilities:
                return agent

        return None
