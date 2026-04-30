from __future__ import annotations

from threading import Lock

from .models import TaskSession


class TaskStore:
    def __init__(self) -> None:
        self._lock = Lock()
        self._tasks: dict[str, TaskSession] = {}

    def put(self, task: TaskSession) -> None:
        with self._lock:
            self._tasks[task.task_id] = task

    def get(self, task_id: str) -> TaskSession | None:
        with self._lock:
            return self._tasks.get(task_id)

    def list(self) -> list[TaskSession]:
        with self._lock:
            return list(reversed(list(self._tasks.values())))
