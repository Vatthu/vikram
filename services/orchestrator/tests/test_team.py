from __future__ import annotations

import unittest

from levik_orchestrator.models import AgentProfile
from levik_orchestrator.team import TeamRouter


class TeamRouterTests(unittest.TestCase):
    def test_router_adds_provider_and_model_for_matching_role(self) -> None:
        router = TeamRouter(
            [
                AgentProfile(
                    id="lead-1",
                    role="lead",
                    provider="zhipu",
                    model="glm-5.1",
                    capabilities=["planning", "architecture"],
                )
            ]
        )

        request = router.request("task-001", "lead", "Plan this change")

        self.assertEqual("lead", request.role)
        self.assertEqual("zhipu", request.provider)
        self.assertEqual("glm-5.1", request.model)

    def test_router_falls_back_to_role_only_without_roster_match(self) -> None:
        router = TeamRouter(
            [
                AgentProfile(
                    id="reviewer-1",
                    role="reviewer",
                    provider="deepseek",
                    model="deepseek-reasoner",
                )
            ]
        )

        request = router.request("task-001", "runner", "Analyze verification")

        self.assertEqual("runner", request.role)
        self.assertIsNone(request.provider)
        self.assertIsNone(request.model)


if __name__ == "__main__":
    unittest.main()
