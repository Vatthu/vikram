from __future__ import annotations

import unittest

import httpx

from vikram_orchestrator.host_client import HostClient
from vikram_orchestrator.models import FileReadRequest


class HostClientTests(unittest.TestCase):
    def test_read_file_includes_response_body_on_http_error(self) -> None:
        def handler(_: httpx.Request) -> httpx.Response:
            return httpx.Response(
                400,
                json={"error": "worktree_path must remain inside the managed worktree root"},
            )

        client = HostClient.__new__(HostClient)
        client.socket_path = "/tmp/vikramd.sock"
        client._client = httpx.Client(
            transport=httpx.MockTransport(handler),
            base_url="http://vikramd",
        )

        with self.assertRaises(httpx.HTTPStatusError) as ctx:
            client.read_file(
                FileReadRequest(
                    task_id="task-001",
                    worktree_path="/tmp/worktrees/task-001",
                    path="README.md",
                    max_bytes=1024,
                )
            )

        self.assertIn("Response body:", str(ctx.exception))
        self.assertIn("worktree_path must remain inside the managed worktree root", str(ctx.exception))


if __name__ == "__main__":
    unittest.main()
