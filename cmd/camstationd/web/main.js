async function getJSON(url, options) {
  const response = await fetch(url, options);
  const data = await response.json();
  if (!response.ok) {
    throw new Error(data.error || `HTTP ${response.status}`);
  }
  return data;
}

function renderJSON(node, value) {
  node.textContent = JSON.stringify(value, null, 2);
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function playerURL(streamName) {
  return `/live/stream.html?src=${encodeURIComponent(streamName)}`;
}

async function refresh() {
  const health = document.querySelector("#health");
  const events = document.querySelector("#events");
  const cameras = document.querySelector("#cameras");

  try {
    renderJSON(health, await getJSON("/api/health"));
  } catch (error) {
    health.textContent = error.message;
  }

  try {
    const rows = await getJSON("/api/cameras");
    cameras.innerHTML = rows.length
      ? rows
          .map((camera) => {
            const url = playerURL(camera.streamName);
            const probe = camera.lastProbe ? JSON.stringify(camera.lastProbe) : "";
            return `<div class="camera">
              <div>
                <strong>${escapeHTML(camera.name)}</strong>
                <span class="state ${escapeHTML(camera.state)}">${escapeHTML(camera.state)}</span>
                <p>${escapeHTML(camera.redactedUrl)}</p>
                <code>${escapeHTML(probe)}</code>
              </div>
              <a class="button" href="${escapeHTML(url)}" target="_blank" rel="noreferrer">Open Live</a>
            </div>`;
          })
          .join("")
      : "No cameras registered.";
  } catch (error) {
    cameras.textContent = error.message;
  }

  try {
    const rows = await getJSON("/api/events");
    events.innerHTML = rows
      .map((event) => {
        const details = event.details ? JSON.stringify(event.details) : "";
        return `<div class="event ${event.level}">
          <time>${escapeHTML(event.createdAt)}</time>
          <strong>${escapeHTML(event.source)}</strong>
          <span>${escapeHTML(event.message)}</span>
          <code>${escapeHTML(details)}</code>
        </div>`;
      })
      .join("");
  } catch (error) {
    events.textContent = error.message;
  }
}

document.querySelector("#refresh").addEventListener("click", refresh);
document.querySelector("#cameraForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = document.querySelector("#probeResult");
  output.textContent = "Saving and probing...";
  try {
    const data = await getJSON("/api/cameras", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        name: document.querySelector("#cameraName").value,
        url: document.querySelector("#cameraUrl").value,
      }),
    });
    renderJSON(output, data);
    await refresh();
  } catch (error) {
    output.textContent = error.message;
    await refresh();
  }
});

refresh();
