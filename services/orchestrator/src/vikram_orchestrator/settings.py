from __future__ import annotations

from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    orchestrator_socket: str = "/tmp/vikram-orchestrator.sock"
    host_socket: str = "/tmp/vikramd.sock"
    state_dir: Path = Path.home() / ".vikram" / "orchestrator"
    checkpoint_db: Path = Path.home() / ".vikram" / "db" / "orchestrator.sqlite"

    model_config = SettingsConfigDict(
        env_prefix="VIKRAM_",
        env_file=".env",
        env_file_encoding="utf-8",
    )


settings = Settings()
