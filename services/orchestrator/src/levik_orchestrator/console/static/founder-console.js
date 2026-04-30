const state = {
  tasks: [],
  selectedTaskId: null,
  currentReview: null,
  currentArtifactPath: null,
  currentArtifactContent: null,
};

const els = {
  taskList: document.getElementById("task-list"),
  refreshTasks: document.getElementById("refresh-tasks"),
  filterStatus: document.getElementById("filter-status"),
  filterPhase: document.getElementById("filter-phase"),
  filterNeedsReview: document.getElementById("filter-needs-review"),
  filterFollowUp: document.getElementById("filter-follow-up"),
  metricReviewCount: document.getElementById("metric-review-count"),
  metricFollowUpCount: document.getElementById("metric-follow-up-count"),
  metricCurrentTask: document.getElementById("metric-current-task"),
  detailTitle: document.getElementById("detail-title"),
  detailBadge: document.getElementById("detail-badge"),
  detailSummary: document.getElementById("detail-summary"),
  flash: document.getElementById("flash"),
  taskObjective: document.getElementById("task-objective"),
  taskStatus: document.getElementById("task-status"),
  taskPhase: document.getElementById("task-phase"),
  taskRisk: document.getElementById("task-risk"),
  taskRoute: document.getElementById("task-route"),
  taskSummary: document.getElementById("task-summary"),
  approvalRequest: document.getElementById("approval-request"),
  followUp: document.getElementById("follow-up"),
  mergeReadiness: document.getElementById("merge-readiness"),
  artifactList: document.getElementById("artifact-list"),
  artifactContentMeta: document.getElementById("artifact-content-meta"),
  artifactContent: document.getElementById("artifact-content"),
  changeEvidence: document.getElementById("change-evidence"),
  verificationEvidence: document.getElementById("verification-evidence"),
  artifactPreviews: document.getElementById("artifact-previews"),
  decisionPanel: document.getElementById("decision-panel"),
  decisionState: document.getElementById("decision-state"),
  decisionComment: document.getElementById("decision-comment"),
  decisionProposedEdits: document.getElementById("decision-proposed-edits"),
  decisionButtons: Array.from(document.querySelectorAll("[data-decision]")),
};

function setFlash(kind, message) {
  if (!message) {
    els.flash.className = "flash hidden";
    els.flash.textContent = "";
    return;
  }
  els.flash.className = `flash ${kind}`;
  els.flash.textContent = message;
}

function taskQuery() {
  const params = new URLSearchParams();
  if (els.filterStatus.value) {
    params.set("status", els.filterStatus.value);
  }
  if (els.filterPhase.value.trim()) {
    params.set("phase", els.filterPhase.value.trim());
  }
  if (els.filterNeedsReview.checked) {
    params.set("needs_review", "true");
  }
  if (els.filterFollowUp.checked) {
    params.set("follow_up_required", "true");
  }
  return params.toString() ? `?${params.toString()}` : "";
}

async function loadTasks(preserveSelection = true) {
  const response = await fetch(`/v1/tasks${taskQuery()}`);
  if (!response.ok) {
    throw new Error(`Task list failed: ${response.status}`);
  }
  state.tasks = await response.json();
  renderTaskList();
  renderMetrics();

  if (state.tasks.length === 0) {
    state.selectedTaskId = null;
    state.currentArtifactPath = null;
    state.currentArtifactContent = null;
    renderReview(null);
    return;
  }

  if (
    !preserveSelection ||
    !state.selectedTaskId ||
    !state.tasks.some((task) => task.task_id === state.selectedTaskId)
  ) {
    state.selectedTaskId = state.tasks[0].task_id;
  }

  await loadReview(state.selectedTaskId);
}

function renderTaskList() {
  if (state.tasks.length === 0) {
    els.taskList.innerHTML =
      '<div class="empty-state">No tasks match the current filters.</div>';
    return;
  }

  els.taskList.innerHTML = "";
  for (const task of state.tasks) {
    const card = document.createElement("button");
    card.type = "button";
    card.className = `task-card${task.task_id === state.selectedTaskId ? " active" : ""}`;
    card.innerHTML = `
      <h3>${escapeHtml(task.task_id)}</h3>
      <div class="task-meta">
        <span class="status-pill ${task.status === "awaiting_approval" ? "pill-review" : ""}">${escapeHtml(task.status)}</span>
        <span>${escapeHtml(task.phase)}</span>
        ${task.follow_up_required ? '<span class="status-pill pill-follow-up">follow-up</span>' : ""}
        ${task.merge_readiness === "ready" ? '<span class="status-pill pill-merge-ready">merge-ready</span>' : ""}
        ${task.merge_readiness === "blocked" ? '<span class="status-pill pill-merge-blocked">merge-blocked</span>' : ""}
      </div>
      <div class="task-summary">${escapeHtml(task.summary || task.objective)}</div>
    `;
    card.addEventListener("click", async () => {
      state.selectedTaskId = task.task_id;
      renderTaskList();
      await loadReview(task.task_id);
    });
    els.taskList.appendChild(card);
  }
}

function renderMetrics() {
  const reviewCount = state.tasks.filter((task) => task.status === "awaiting_approval").length;
  const followUpCount = state.tasks.filter((task) => task.follow_up_required).length;
  els.metricReviewCount.textContent = String(reviewCount);
  els.metricFollowUpCount.textContent = String(followUpCount);
  els.metricCurrentTask.textContent = state.selectedTaskId || "None";
}

async function loadReview(taskId) {
  if (!taskId) {
    renderReview(null);
    return;
  }
  if (!state.currentReview || state.currentReview.task?.task_id !== taskId) {
    state.currentArtifactPath = null;
    state.currentArtifactContent = null;
  }
  const response = await fetch(`/v1/tasks/${encodeURIComponent(taskId)}/review`);
  if (!response.ok) {
    throw new Error(`Review load failed: ${response.status}`);
  }
  state.currentReview = await response.json();
  renderReview(state.currentReview);
  await ensureArtifactContentLoaded(state.currentReview);
}

function renderReview(review) {
  if (!review) {
    state.currentArtifactPath = null;
    state.currentArtifactContent = null;
    els.detailTitle.textContent = "Select a task";
    els.detailBadge.className = "status-badge subtle";
    els.detailBadge.textContent = "Idle";
    els.detailSummary.classList.add("hidden");
    els.decisionPanel.classList.add("hidden");
    els.metricCurrentTask.textContent = "None";
    renderArtifactContent(null);
    return;
  }

  const { task } = review;
  els.metricCurrentTask.textContent = task.task_id;
  els.detailTitle.textContent = task.task_id;
  els.detailBadge.className = `status-badge ${task.requires_founder_review ? "live" : "subtle"}`;
  if (task.status === "awaiting_approval") {
    els.detailBadge.textContent = "Review required";
  } else if (task.merge_readiness === "ready") {
    els.detailBadge.textContent = "Merge ready";
  } else if (task.merge_readiness === "blocked") {
    els.detailBadge.textContent = "Merge blocked";
  } else {
    els.detailBadge.textContent = task.status;
  }
  els.detailSummary.classList.remove("hidden");
  els.decisionPanel.classList.remove("hidden");

  els.taskObjective.textContent = task.objective;
  els.taskStatus.textContent = task.status;
  els.taskPhase.textContent = task.phase;
  els.taskRisk.textContent = task.risk_class || "n/a";
  els.taskRoute.textContent = task.approval_route || "n/a";
  els.taskSummary.textContent = task.summary;

  renderApprovalRequest(review);
  renderFollowUp(review);
  renderMergeReadiness(review);
  renderArtifacts(review);
  renderArtifactContent(state.currentArtifactContent);
  renderChangeEvidence(review);
  renderVerificationEvidence(review);
  renderArtifactPreviews(review);
  renderDecisionPanel(review);
}

function reviewArtifactEntries(review) {
  return [
    ["Change", review.change_artifact_path],
    ["Verification", review.verification_result_artifact_path],
    ["Approval", review.approval_artifact_path],
    ["Founder decision", review.founder_decision_artifact_path],
    ["Merge readiness", review.merge_artifact_path],
  ].filter(([, path]) => path);
}

async function ensureArtifactContentLoaded(review) {
  const artifacts = reviewArtifactEntries(review);
  if (artifacts.length === 0 || !review.task) {
    state.currentArtifactPath = null;
    state.currentArtifactContent = null;
    renderArtifacts(review);
    renderArtifactContent(null);
    return;
  }

  const activePath = artifacts.some(([, path]) => path === state.currentArtifactPath)
    ? state.currentArtifactPath
    : artifacts[0][1];

  if (
    state.currentArtifactContent &&
    state.currentArtifactContent.path === activePath
  ) {
    state.currentArtifactPath = activePath;
    renderArtifacts(review);
    renderArtifactContent(state.currentArtifactContent);
    return;
  }

  await loadArtifactContent(review.task.task_id, activePath);
}

function renderApprovalRequest(review) {
  const approval = review.approval_request;
  if (!approval) {
    els.approvalRequest.innerHTML =
      '<div class="empty-state">No active approval request for this task.</div>';
    return;
  }

  els.approvalRequest.innerHTML = `
    <div><strong>${escapeHtml(approval.summary)}</strong></div>
    <div>Risk class: ${escapeHtml(approval.risk_class)}</div>
    <div>Route: ${escapeHtml(approval.route)}</div>
    <div>Reasons:</div>
    <pre>${escapeHtml((approval.reasons || []).join("\n") || "none")}</pre>
    <div>Available decisions:</div>
    <pre>${escapeHtml((approval.options || []).join(", "))}</pre>
  `;
}

function renderFollowUp(review) {
  const followUp = review.follow_up;
  if (!followUp || !followUp.required) {
    els.followUp.innerHTML =
      '<div class="empty-state">No pending follow-up context.</div>';
    return;
  }

  const proposed =
    followUp.proposed_edits && Object.keys(followUp.proposed_edits).length > 0
      ? JSON.stringify(followUp.proposed_edits, null, 2)
      : "none";

  els.followUp.innerHTML = `
    <div><strong>${escapeHtml(followUp.phase || "follow-up")}</strong></div>
    <pre>${escapeHtml(followUp.comment || "No comment")}</pre>
    <div>Proposed edits:</div>
    <pre>${escapeHtml(proposed)}</pre>
  `;
}

function renderMergeReadiness(review) {
  const merge = review.merge_assessment;
  if (!merge || merge.state === "unknown") {
    els.mergeReadiness.innerHTML =
      '<div class="empty-state">Merge readiness has not been assessed yet.</div>';
    return;
  }

  const blockers = (merge.blockers || []).join("\n") || "none";
  const notes = (merge.notes || []).join("\n") || "none";
  const statusLines = (merge.status_lines || []).join("\n") || "none";
  const changedFiles =
    (merge.changed_files || [])
      .map((file) => {
        const stat = file.binary ? "binary" : `+${file.additions || 0}/-${file.deletions || 0}`;
        return `${file.status || "changed"} ${file.path} (${stat})`;
      })
      .join("\n") || "none";

  els.mergeReadiness.innerHTML = `
    <div><strong>${escapeHtml(merge.summary || merge.state)}</strong></div>
    <div>State: ${escapeHtml(merge.state)}</div>
    <div>Branch: ${escapeHtml(merge.branch || "n/a")}</div>
    <div>HEAD: ${escapeHtml((merge.head_ref || "").slice(0, 12) || "n/a")}</div>
    <div>Diff: ${escapeHtml(merge.diff_short_stat || `${merge.changed_file_count || 0} files, +${merge.additions || 0}/-${merge.deletions || 0}`)}</div>
    <div>Blockers:</div>
    <pre>${escapeHtml(blockers)}</pre>
    <div>Notes:</div>
    <pre>${escapeHtml(notes)}</pre>
    <div>Changed files:</div>
    <pre>${escapeHtml(changedFiles)}</pre>
    <div>Git status:</div>
    <pre>${escapeHtml(statusLines)}</pre>
  `;
}

function renderArtifacts(review) {
  const artifacts = reviewArtifactEntries(review);

  if (artifacts.length === 0) {
    els.artifactList.innerHTML =
      '<li class="empty-state">No artifact paths recorded yet.</li>';
    return;
  }

  els.artifactList.innerHTML = artifacts
    .map(
      ([label, path]) => `
        <li>
          <button
            class="artifact-button ${path === state.currentArtifactPath ? "active" : ""}"
            type="button"
            data-artifact-path="${escapeHtml(path)}"
          >
            <span class="artifact-label">${escapeHtml(label)}</span>
            <pre class="artifact-path">${escapeHtml(path)}</pre>
          </button>
        </li>
      `,
    )
    .join("");

  for (const button of els.artifactList.querySelectorAll("[data-artifact-path]")) {
    button.addEventListener("click", async () => {
      if (!state.currentReview || !state.currentReview.task) {
        return;
      }
      try {
        await loadArtifactContent(
          state.currentReview.task.task_id,
          button.dataset.artifactPath || "",
        );
      } catch (error) {
        setFlash("error", error.message);
      }
    });
  }
}

function renderArtifactContent(artifactContent) {
  if (!artifactContent) {
    els.artifactContentMeta.textContent = "";
    els.artifactContent.textContent = "Select an artifact to load its full content.";
    return;
  }

  els.artifactContentMeta.textContent = `${artifactContent.path} · ${artifactContent.bytes_read} bytes${artifactContent.truncated ? " · truncated" : ""}`;
  els.artifactContent.textContent = artifactContent.content || "Artifact is empty.";
}

async function loadArtifactContent(taskId, artifactPath) {
  if (!artifactPath) {
    state.currentArtifactPath = null;
    state.currentArtifactContent = null;
    renderArtifactContent(null);
    return;
  }

  const response = await fetch(
    `/v1/tasks/${encodeURIComponent(taskId)}/artifacts/content?path=${encodeURIComponent(artifactPath)}`,
  );
  if (!response.ok) {
    const message = await response.text();
    throw new Error(`Artifact load failed: ${response.status} ${message}`);
  }

  state.currentArtifactPath = artifactPath;
  state.currentArtifactContent = await response.json();
  renderArtifacts(state.currentReview);
  renderArtifactContent(state.currentArtifactContent);
}

function renderChangeEvidence(review) {
  const edits = review.applied_edits || [];
  if (edits.length === 0) {
    els.changeEvidence.innerHTML =
      '<div class="empty-state">No applied edit evidence recorded yet.</div>';
    return;
  }

  els.changeEvidence.innerHTML = edits
    .map(
      (edit) => `
        <div class="evidence-block">
          <div><strong>${escapeHtml(edit.path)}</strong></div>
          <div class="task-summary">${escapeHtml(edit.rationale || "No rationale recorded")}</div>
          <pre class="json-box">${escapeHtml(edit.diff_preview || "No diff preview")}</pre>
        </div>
      `,
    )
    .join("");
}

function renderVerificationEvidence(review) {
  const runs = review.verification_runs || [];
  if (runs.length === 0) {
    els.verificationEvidence.innerHTML =
      '<div class="empty-state">No verification runs recorded yet.</div>';
    return;
  }

  els.verificationEvidence.innerHTML = runs
    .map(
      (run) => `
        <div class="evidence-block">
          <div><strong>${escapeHtml(run.command)}</strong></div>
          <div class="task-summary">${escapeHtml(run.summary || (run.success ? "passed" : "failed"))}</div>
          <pre class="json-box">${escapeHtml(run.output_preview || "No command output preview")}</pre>
        </div>
      `,
    )
    .join("");
}

function renderArtifactPreviews(review) {
  const previews = review.artifact_previews || [];
  if (previews.length === 0) {
    els.artifactPreviews.innerHTML =
      '<div class="empty-state">No artifact previews available yet.</div>';
    return;
  }

  els.artifactPreviews.innerHTML = previews
    .map(
      (preview) => `
        <div class="evidence-block">
          <div><strong>${escapeHtml(preview.title)}</strong></div>
          <div class="task-summary">${escapeHtml(preview.kind)}${preview.path ? ` · ${escapeHtml(preview.path)}` : ""}</div>
          <pre class="json-box">${escapeHtml(preview.content_preview || "No preview content")}</pre>
        </div>
      `,
    )
    .join("");
}

function renderDecisionPanel(review) {
  const enabled = review.can_resume;
  els.decisionState.textContent = enabled
    ? "Ready for founder decision"
    : "This task is not currently awaiting a founder decision";

  for (const button of els.decisionButtons) {
    button.disabled = !enabled;
  }
}

async function submitDecision(decision) {
  if (!state.currentReview || !state.currentReview.task) {
    return;
  }

  const taskId = state.currentReview.task.task_id;
  let proposedEdits = {};
  const rawProposedEdits = els.decisionProposedEdits.value.trim();
  if (rawProposedEdits) {
    try {
      proposedEdits = JSON.parse(rawProposedEdits);
    } catch (error) {
      setFlash("error", `Invalid proposed edits JSON: ${error.message}`);
      return;
    }
  }

  setFlash("info", `Sending ${decision} for ${taskId}...`);

  const response = await fetch(`/v1/tasks/${encodeURIComponent(taskId)}/resume`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      task_id: taskId,
      decision,
      comment: els.decisionComment.value,
      proposed_edits: proposedEdits,
    }),
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(`Decision failed: ${response.status} ${message}`);
  }

  const updatedTask = await response.json();
  els.decisionComment.value = "";
  els.decisionProposedEdits.value = "";
  setFlash("info", `Task ${updatedTask.task_id} is now ${updatedTask.phase}.`);
  await loadTasks(true);
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

async function bootstrap() {
  try {
    await loadTasks(false);
  } catch (error) {
    setFlash("error", error.message);
  }
}

els.refreshTasks.addEventListener("click", async () => {
  try {
    await loadTasks(true);
    setFlash("info", "Task list refreshed.");
  } catch (error) {
    setFlash("error", error.message);
  }
});

for (const element of [
  els.filterStatus,
  els.filterPhase,
  els.filterNeedsReview,
  els.filterFollowUp,
]) {
  element.addEventListener("change", async () => {
    try {
      await loadTasks(false);
    } catch (error) {
      setFlash("error", error.message);
    }
  });
}

for (const button of els.decisionButtons) {
  button.addEventListener("click", async () => {
    try {
      await submitDecision(button.dataset.decision);
    } catch (error) {
      setFlash("error", error.message);
    }
  });
}

bootstrap();
