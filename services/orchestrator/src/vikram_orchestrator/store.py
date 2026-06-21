from __future__ import annotations

import json
from pathlib import Path
from threading import Lock

from .models import TaskSession


class TaskStore:
    def __init__(self, storage_path: Path | None = None) -> None:
        self._lock = Lock()
        self._tasks: dict[str, TaskSession] = {}
        self._storage_path = storage_path
        if self._storage_path is not None:
            self._load()

    def put(self, task: TaskSession) -> None:
        with self._lock:
            self._tasks[task.task_id] = task
            self._save()

    def get(self, task_id: str) -> TaskSession | None:
        with self._lock:
            return self._tasks.get(task_id)

    def list(self) -> list[TaskSession]:
        with self._lock:
            return list(reversed(list(self._tasks.values())))

    def _load(self) -> None:
        if self._storage_path is None or not self._storage_path.exists():
            return

        try:
            raw = json.loads(self._storage_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            return

        if not isinstance(raw, list):
            return

        loaded: dict[str, TaskSession] = {}
        for item in raw:
            if not isinstance(item, dict):
                continue
            try:
                task = TaskSession.model_validate(item)
            except Exception:
                continue
            loaded[task.task_id] = task

        self._tasks = loaded

    def _save(self) -> None:
        if self._storage_path is None:
            return

        self._storage_path.parent.mkdir(parents=True, exist_ok=True)
        payload = [task.model_dump(mode="json") for task in self._tasks.values()]
        tmp_path = self._storage_path.with_suffix(self._storage_path.suffix + ".tmp")
        tmp_path.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
        tmp_path.replace(self._storage_path)
