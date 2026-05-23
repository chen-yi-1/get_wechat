from __future__ import annotations

import sqlite3
from threading import RLock
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


@dataclass(frozen=True)
class Session:
    session_id: int
    title: str
    created_at: str
    updated_at: str


class MemoryStore:
    def __init__(self, db_path: Path):
        self._db_path = db_path
        self._lock = RLock()
        self._conn = sqlite3.connect(self._db_path, check_same_thread=False)
        self._conn.row_factory = sqlite3.Row
        self._init_schema()

    def _init_schema(self) -> None:
        with self._lock:
            self._conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS sessions (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    title TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS messages (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    session_id INTEGER NOT NULL,
                    role TEXT NOT NULL CHECK(role IN ('system', 'user', 'assistant')),
                    content TEXT NOT NULL,
                    created_at TEXT NOT NULL,
                    FOREIGN KEY (session_id) REFERENCES sessions(id)
                );

                CREATE TABLE IF NOT EXISTS summaries (
                    session_id INTEGER PRIMARY KEY,
                    summary TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    FOREIGN KEY (session_id) REFERENCES sessions(id)
                );
                """
            )
            self._conn.commit()

    def create_session(self, title: str) -> Session:
        now = _utc_now()
        with self._lock:
            cursor = self._conn.execute(
                "INSERT INTO sessions (title, created_at, updated_at) VALUES (?, ?, ?)",
                (title, now, now),
            )
            self._conn.commit()
            session_id = int(cursor.lastrowid)
        return self.get_session(session_id)

    def get_session(self, session_id: int) -> Session:
        with self._lock:
            row = self._conn.execute(
                "SELECT id, title, created_at, updated_at FROM sessions WHERE id = ?",
                (session_id,),
            ).fetchone()
        if row is None:
            raise ValueError(f"会话不存在: {session_id}")
        return Session(
            session_id=int(row["id"]),
            title=row["title"],
            created_at=row["created_at"],
            updated_at=row["updated_at"],
        )

    def list_sessions(self, limit: int = 20) -> list[Session]:
        with self._lock:
            rows = self._conn.execute(
                """
                SELECT id, title, created_at, updated_at
                FROM sessions
                ORDER BY updated_at DESC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [
            Session(
                session_id=int(row["id"]),
                title=row["title"],
                created_at=row["created_at"],
                updated_at=row["updated_at"],
            )
            for row in rows
        ]

    def append_message(self, session_id: int, role: str, content: str) -> None:
        now = _utc_now()
        with self._lock:
            self._conn.execute(
                "INSERT INTO messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)",
                (session_id, role, content, now),
            )
            self._conn.execute(
                "UPDATE sessions SET updated_at = ? WHERE id = ?",
                (now, session_id),
            )
            self._conn.commit()

    def recent_messages(self, session_id: int, limit: int = 20) -> list[dict[str, str]]:
        with self._lock:
            rows = self._conn.execute(
                """
                SELECT role, content
                FROM messages
                WHERE session_id = ?
                ORDER BY id DESC
                LIMIT ?
                """,
                (session_id, limit),
            ).fetchall()
        return [{"role": row["role"], "content": row["content"]} for row in reversed(rows)]

    def total_message_count(self, session_id: int) -> int:
        with self._lock:
            row = self._conn.execute(
                "SELECT COUNT(*) AS cnt FROM messages WHERE session_id = ?",
                (session_id,),
            ).fetchone()
        return int(row["cnt"])

    def get_summary(self, session_id: int) -> str:
        with self._lock:
            row = self._conn.execute(
                "SELECT summary FROM summaries WHERE session_id = ?",
                (session_id,),
            ).fetchone()
        return row["summary"] if row else ""

    def upsert_summary(self, session_id: int, summary: str) -> None:
        now = _utc_now()
        with self._lock:
            self._conn.execute(
                """
                INSERT INTO summaries (session_id, summary, updated_at)
                VALUES (?, ?, ?)
                ON CONFLICT(session_id) DO UPDATE SET
                    summary = excluded.summary,
                    updated_at = excluded.updated_at
                """,
                (session_id, summary, now),
            )
            self._conn.commit()

    def update_last_assistant_message(self, session_id: int, content: str) -> None:
        with self._lock:
            row = self._conn.execute(
                """
                SELECT id
                FROM messages
                WHERE session_id = ? AND role = 'assistant'
                ORDER BY id DESC
                LIMIT 1
                """,
                (session_id,),
            ).fetchone()
            if row is None:
                raise ValueError(f"会话不存在可更新的 assistant 消息: {session_id}")
            self._conn.execute(
                "UPDATE messages SET content = ? WHERE id = ?",
                (content, int(row["id"])),
            )
            self._conn.commit()

    def delete_session(self, session_id: int) -> None:
        with self._lock:
            cursor = self._conn.execute("DELETE FROM sessions WHERE id = ?", (session_id,))
            if cursor.rowcount == 0:
                raise ValueError(f"会话不存在: {session_id}")
            self._conn.execute("DELETE FROM messages WHERE session_id = ?", (session_id,))
            self._conn.execute("DELETE FROM summaries WHERE session_id = ?", (session_id,))
            self._conn.commit()

    def close(self) -> None:
        with self._lock:
            self._conn.close()
