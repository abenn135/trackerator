function openParentSubtasks(task) {
  for (let parent = task.parentElement; parent; parent = parent.parentElement) {
    if (parent.matches("details.subtasks")) {
      parent.open = true;
    }
  }
}

const localTimestampFormatter = new Intl.DateTimeFormat(undefined, {
  year: "numeric", month: "short", day: "numeric",
  hour: "numeric", minute: "2-digit", second: "2-digit", timeZoneName: "short",
});

for (const timestamp of document.querySelectorAll("time.created-at[datetime]")) {
  const date = new Date(timestamp.getAttribute("datetime"));
  if (!Number.isNaN(date.getTime())) {
    timestamp.textContent = localTimestampFormatter.format(date);
  }
}

function openSubtaskIDs() {
  return Array.from(document.querySelectorAll("details.subtasks[data-task-id]"))
    .filter((section) => section.open)
    .map((section) => section.dataset.taskId)
    .join(",");
}

document.addEventListener("toggle", (event) => {
  if (!(event.target instanceof Element) || !event.target.matches("details.subtasks[data-task-id]")) return;
  const url = new URL(window.location.href);
  const hashTask = /^#task-[0-9]+$/.test(url.hash) ? document.getElementById(url.hash.slice(1)) : null;
  if (!event.target.open && hashTask && event.target.contains(hashTask)) {
    url.hash = "";
  }
  url.searchParams.set("open", openSubtaskIDs());
  window.history.replaceState(null, "", url.pathname + url.search + url.hash);
}, true);

document.addEventListener("submit", (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement) || form.method.toLowerCase() !== "post") return;
  let field = form.querySelector('input[name="open"]');
  if (!field) {
    field = document.createElement("input");
    field.type = "hidden";
    field.name = "open";
    form.append(field);
  }
  field.value = openSubtaskIDs();
});

function revealHashTask() {
  const hash = window.location.hash;
  if (!/^#task-[0-9]+$/.test(hash)) return;
  const task = document.getElementById(hash.slice(1));
  if (!task) return;
  openParentSubtasks(task);
  requestAnimationFrame(() => task.scrollIntoView({ block: "start" }));
}

document.addEventListener("click", (event) => {
  if (!(event.target instanceof Element)) return;

  const button = event.target.closest("button[data-reveal-target]");
  if (button) {
    const panel = document.getElementById(button.dataset.revealTarget);
    if (!panel) return;
    const expanded = button.getAttribute("aria-expanded") === "true";
    panel.hidden = expanded;
    button.setAttribute("aria-expanded", String(!expanded));
    if (!expanded) panel.querySelector("input:not([type=hidden])")?.focus();
    return;
  }

  const link = event.target.closest('a[href^="#task-"]');
  if (link) {
    const task = document.getElementById(link.hash.slice(1));
    if (task) openParentSubtasks(task);
  }
});

window.addEventListener("hashchange", revealHashTask);
revealHashTask();
