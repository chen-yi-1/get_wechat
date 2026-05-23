let currentSessionId = null;
let mermaidInitialized = false;

async function request(url, options = {}) {
  const resp = await fetch(url, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (!resp.ok) {
    const text = await resp.text();
    throw new Error(text || `请求失败: ${resp.status}`);
  }
  return resp.json();
}

function escapeHtml(text) {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

async function renderMessages(messages) {
  const el = document.getElementById("messages");
  el.innerHTML = "";
  for (const m of messages) {
    const item = document.createElement("div");
    item.className = `message ${m.role}`;
    if (m.role === "assistant") {
      await renderAssistantContent(item, m.content || "");
    } else {
      item.innerHTML = escapeHtml(m.content || "");
    }
    el.appendChild(item);
  }
  el.scrollTop = el.scrollHeight;
}

function initMermaid() {
  if (mermaidInitialized || typeof mermaid === "undefined") return;
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: "default",
  });
  mermaidInitialized = true;
}

function parseMermaidBlocks(text) {
  const regex = /```mermaid\s*([\s\S]*?)```/gi;
  const blocks = [];
  let lastIndex = 0;
  let match = null;
  while ((match = regex.exec(text)) !== null) {
    if (match.index > lastIndex) {
      blocks.push({ type: "text", value: text.slice(lastIndex, match.index) });
    }
    blocks.push({ type: "mermaid", value: match[1].trim() });
    lastIndex = regex.lastIndex;
  }
  if (lastIndex < text.length) {
    blocks.push({ type: "text", value: text.slice(lastIndex) });
  }
  return blocks;
}

async function renderAssistantContent(container, text) {
  initMermaid();
  const blocks = parseMermaidBlocks(text);
  if (blocks.length === 0) {
    container.innerHTML = escapeHtml(text);
    return { ok: true, error: "" };
  }

  container.innerHTML = "";
  for (const block of blocks) {
    if (block.type === "text" && block.value.trim()) {
      const textEl = document.createElement("div");
      textEl.innerHTML = escapeHtml(block.value);
      container.appendChild(textEl);
      continue;
    }
    if (block.type === "mermaid") {
      const wrap = document.createElement("div");
      wrap.className = "mermaid-wrap";
      const diagramEl = document.createElement("div");
      diagramEl.className = "mermaid";
      diagramEl.textContent = block.value;
      wrap.appendChild(diagramEl);
      container.appendChild(wrap);
    }
  }

  if (typeof mermaid !== "undefined") {
    try {
      await mermaid.run({ nodes: container.querySelectorAll(".mermaid") });
      return { ok: true, error: "" };
    } catch (err) {
      const message = err?.message || String(err);
      const warn = document.createElement("div");
      warn.className = "mermaid-warn";
      warn.textContent = `Mermaid 渲染失败：${message}`;
      container.appendChild(warn);
      return { ok: false, error: message };
    }
  }
  return { ok: true, error: "" };
}

async function loadMessages(sessionId) {
  const messages = await request(`/api/sessions/${sessionId}/messages`);
  await renderMessages(messages);
}

function formatTime(isoTime) {
  const d = new Date(isoTime);
  return d.toLocaleString();
}

async function loadSessions(selectLatest = true) {
  const sessions = await request("/api/sessions");
  const list = document.getElementById("sessionList");
  list.innerHTML = "";

  for (const s of sessions) {
    const item = document.createElement("div");
    item.className = "session-item";
    if (s.id === currentSessionId) {
      item.classList.add("active");
    }
    item.innerHTML = `
      <div class="session-main">
        <div class="session-title">${escapeHtml(s.title)}</div>
        <div class="session-time">${formatTime(s.updated_at)}</div>
      </div>
      <button class="session-delete" title="删除会话">×</button>
    `;
    item.onclick = async () => {
      currentSessionId = s.id;
      document.getElementById("chatTitle").textContent = s.title;
      await loadSessions(false);
      await loadMessages(s.id);
    };
    item.querySelector(".session-delete").onclick = async (e) => {
      e.stopPropagation();
      const ok = confirm(`确认删除会话「${s.title}」吗？`);
      if (!ok) return;
      await request(`/api/sessions/${s.id}`, { method: "DELETE" });
      if (currentSessionId === s.id) {
        currentSessionId = null;
        document.getElementById("chatTitle").textContent = "请选择会话";
        renderMessages([]);
      }
      await loadSessions(true);
    };
    list.appendChild(item);
  }

  if (sessions.length === 0) {
    await createSession("默认会话");
    return;
  }

  if (selectLatest || currentSessionId === null) {
    currentSessionId = sessions[0].id;
    document.getElementById("chatTitle").textContent = sessions[0].title;
    await loadMessages(currentSessionId);
    await loadSessions(false);
  }
}

async function createSession(title = "新会话") {
  const session = await request("/api/sessions", {
    method: "POST",
    body: JSON.stringify({ title }),
  });
  currentSessionId = session.id;
  document.getElementById("chatTitle").textContent = session.title;
  await loadSessions(false);
  renderMessages([]);
}

async function sendMessage(content) {
  if (!currentSessionId) return;
  const messagesEl = document.getElementById("messages");

  const userEl = document.createElement("div");
  userEl.className = "message user";
  userEl.innerHTML = escapeHtml(content);
  messagesEl.appendChild(userEl);

  const waitingEl = document.createElement("div");
  waitingEl.className = "message assistant";
  waitingEl.textContent = "思考中...";
  messagesEl.appendChild(waitingEl);
  messagesEl.scrollTop = messagesEl.scrollHeight;

  const data = await request(`/api/sessions/${currentSessionId}/messages`, {
    method: "POST",
    body: JSON.stringify({ content }),
  });
  let assistantText = data.assistant || "";
  let renderResult = await renderAssistantContent(waitingEl, assistantText);
  if (!renderResult.ok) {
    try {
      const repaired = await request(`/api/sessions/${currentSessionId}/repair-mermaid`, {
        method: "POST",
        body: JSON.stringify({
          original_response: assistantText,
          render_error: renderResult.error || "unknown mermaid render error",
        }),
      });
      assistantText = repaired.assistant || assistantText;
      await renderAssistantContent(waitingEl, assistantText);
    } catch (err) {
      const errEl = document.createElement("div");
      errEl.className = "mermaid-warn";
      errEl.textContent = `自动修复失败：${err.message || String(err)}`;
      waitingEl.appendChild(errEl);
    }
  }

  await loadSessions(false);
}

function bindEvents() {
  document.getElementById("newSessionBtn").onclick = async () => {
    const title = prompt("输入会话标题", "新会话");
    if (title === null) return;
    await createSession(title);
  };

  document.getElementById("memoryBtn").onclick = async () => {
    if (!currentSessionId) return;
    const data = await request(`/api/sessions/${currentSessionId}/memory`);
    alert(data.summary || "当前会话还没有长期记忆");
  };

  const form = document.getElementById("chatForm");
  const input = document.getElementById("messageInput");

  input.addEventListener("keydown", async (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      form.requestSubmit();
    }
  });

  form.onsubmit = async (e) => {
    e.preventDefault();
    const content = input.value.trim();
    if (!content) return;
    const submitBtn = form.querySelector("button[type=submit]");
    submitBtn.disabled = true;
    input.value = "";
    try {
      await sendMessage(content);
    } finally {
      submitBtn.disabled = false;
    }
  };
}

async function bootstrap() {
  bindEvents();
  await loadSessions(true);
}

bootstrap().catch((err) => {
  alert(err.message || String(err));
});
