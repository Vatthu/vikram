from __future__ import annotations

import os

import uvicorn

from .server import build_app
from .settings import settings


def main() -> None:
    socket_path = settings.orchestrator_socket
    os.makedirs(os.path.dirname(socket_path), exist_ok=True)
    if os.path.exists(socket_path):
        os.remove(socket_path)

    uvicorn.run(build_app(), uds=socket_path, log_level="info")


if __name__ == "__main__":
    main()
