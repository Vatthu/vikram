from __future__ import annotations

from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    orchestrator_socket: str = "/tmp/levik-orchestrator.sock"
    host_socket: str = "/tmp/levikd.sock"
    state_dir: Path = Path.home() / ".levik" / "orchestrator"
    checkpoint_db: Path = Path.home() / ".levik" / "db" / "orchestrator.sqlite"

    model_config = SettingsConfigDict(
        env_prefix="LEVIK_",
        env_file=".env",
        env_file_encoding="utf-8",
    )


settings = Settings()
