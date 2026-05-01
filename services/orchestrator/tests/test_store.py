from __future__ import annotations

import tempfile

import unittest
from pathlib import Path

from levik_orchestrator.models import RepoRef, TaskConstraints, TaskSession
from levik_orchestrator.store import TaskStore


class TaskStoreTests(unittest.TestCase):
    def test_store_persists_tasks_to_disk(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            storage_path = Path(tmpdir) / "tasks.json"

            first_store = TaskStore(storage_path)
            first_store.put(
                TaskSession(
                    task_id="task-001",
                    source="console",
                    requested_by="founder",
                    objective="Keep task state after restart",
                    repo=RepoRef(path="/repos/levik", default_branch="main"),
                    constraints=TaskConstraints(),
                    status="running",
                    phase="change_ready",
                    summary="Task accepted by orchestrator",
                )
            )

            second_store = TaskStore(storage_path)
            task = second_store.get("task-001")

            self.assertIsNotNone(task)
            assert task is not None
            self.assertEqual("task-001", task.task_id)
            self.assertEqual("change_ready", task.phase)
            self.assertTrue(storage_path.exists())


if __name__ == "__main__":
    unittest.main()
