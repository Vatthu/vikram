from __future__ import annotations

from contextlib import asynccontextmanager
from pathlib import Path

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

from .host_client import HostClient
from .models import (
    ApprovalDecision,
    ArtifactReadRequest,
    ArtifactReadResponse,
    TaskChangeRequest,
    TaskCreateRequest,
    TaskReviewDetail,
    TaskSession,
)
from .settings import settings
from .store import TaskStore
from .workflow import (
    apply_change_request,
    build_graph,
    close_graph,
    initial_state_from_request,
    resume_from_founder_decision,
    state_to_task_session,
    task_review_from_state,
)


def build_app(
    host_client: HostClient | None = None,
    store: TaskStore | None = None,
    checkpoint_db: Path | None = None,
) -> FastAPI:
    managed_host_client = host_client or HostClient(settings.host_socket)
    graph = build_graph(managed_host_client, checkpoint_db=checkpoint_db)
    task_store = store or TaskStore()
    console_dir = Path(__file__).with_name("console")
    console_static_dir = console_dir / "static"

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        try:
            yield
        finally:
            close_graph(graph)
            managed_host_client.close()

    app = FastAPI(title="LeVik Orchestrator", version="0.1.0", lifespan=lifespan)
    app.mount(
        "/console/static",
        StaticFiles(directory=console_static_dir),
        name="console-static",
    )

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/console", include_in_schema=False)
    @app.get("/console/", include_in_schema=False)
    def founder_console() -> FileResponse:
        return FileResponse(console_dir / "index.html")

    @app.post("/v1/tasks", response_model=TaskSession)
    def create_task(request: TaskCreateRequest) -> TaskSession:
        try:
            result = graph.invoke(
                initial_state_from_request(request),
                config={"configurable": {"thread_id": request.task_id}},
            )
        except httpx.HTTPError as exc:
            raise HTTPException(
                status_code=503,
                detail=f"host daemon unavailable: {exc}",
            ) from exc

        response = state_to_task_session(request, result)
        task_store.put(response)
        return response

    @app.get("/v1/tasks", response_model=list[TaskSession])
    def list_tasks(
        status: str | None = None,
        phase: str | None = None,
        needs_review: bool | None = None,
        follow_up_required: bool | None = None,
    ) -> list[TaskSession]:
        tasks = task_store.list()
        filtered: list[TaskSession] = []
        for task in tasks:
            if status and task.status != status:
                continue
            if phase and task.phase != phase:
                continue
            if needs_review is not None:
                is_waiting_for_review = task.status == "awaiting_approval"
                if is_waiting_for_review != needs_review:
                    continue
            if follow_up_required is not None and task.follow_up_required != follow_up_required:
                continue
            filtered.append(task)
        return filtered

    @app.get("/v1/tasks/{task_id}", response_model=TaskSession)
    def get_task(task_id: str) -> TaskSession:
        task = task_store.get(task_id)
        if task is None:
            raise HTTPException(status_code=404, detail="task not found")
        return task

    @app.get("/v1/tasks/{task_id}/review", response_model=TaskReviewDetail)
    def get_task_review(task_id: str) -> TaskReviewDetail:
        return current_review(task_id)

    @app.get("/v1/tasks/{task_id}/artifacts/content", response_model=ArtifactReadResponse)
    def get_task_artifact_content(
        task_id: str, path: str, max_bytes: int = 32000
    ) -> ArtifactReadResponse:
        review = current_review(task_id)
        allowed_paths = {
            artifact_path
            for artifact_path in [
                review.change_artifact_path,
                review.verification_result_artifact_path,
                review.approval_artifact_path,
                review.founder_decision_artifact_path,
                review.merge_artifact_path,
            ]
            if artifact_path
        }
        if path not in allowed_paths:
            raise HTTPException(status_code=404, detail="artifact not found for task")

        try:
            return managed_host_client.read_artifact(
                ArtifactReadRequest(task_id=task_id, path=path, max_bytes=max_bytes)
            )
        except httpx.HTTPError as exc:
            raise HTTPException(
                status_code=503,
                detail=f"host daemon unavailable: {exc}",
            ) from exc

    @app.post("/v1/tasks/{task_id}/changes", response_model=TaskSession)
    def apply_change(task_id: str, request: TaskChangeRequest) -> TaskSession:
        if request.task_id != task_id:
            raise HTTPException(status_code=400, detail="task_id mismatch")

        task = task_store.get(task_id)
        if task is None:
            raise HTTPException(status_code=404, detail="task not found")

        try:
            response = apply_change_request(graph, task, request)
        except httpx.HTTPError as exc:
            raise HTTPException(
                status_code=503,
                detail=f"host daemon unavailable: {exc}",
            ) from exc

        task_store.put(response)
        return response

    @app.post("/v1/tasks/{task_id}/resume", response_model=TaskSession)
    def resume_task(task_id: str, decision: ApprovalDecision) -> TaskSession:
        if decision.task_id != task_id:
            raise HTTPException(status_code=400, detail="task_id mismatch")

        task = task_store.get(task_id)
        if task is None:
            raise HTTPException(status_code=404, detail="task not found")

        try:
            response = resume_from_founder_decision(graph, task, decision)
        except httpx.HTTPError as exc:
            raise HTTPException(
                status_code=503,
                detail=f"host daemon unavailable: {exc}",
            ) from exc

        task_store.put(response)
        return response

    def current_review(task_id: str) -> TaskReviewDetail:
        task = task_store.get(task_id)
        if task is None:
            raise HTTPException(status_code=404, detail="task not found")

        snapshot = graph.get_state({"configurable": {"thread_id": task_id}})
        state = snapshot.values or {}
        return task_review_from_state(task, state)

    return app
