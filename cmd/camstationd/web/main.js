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

async function refresh() {
  const health = document.querySelector("#health");
  const events = document.querySelector("#events");

  try {
    renderJSON(health, await getJSON("/api/health"));
  } catch (error) {
    health.textContent = error.message;
  }

  try {
    const rows = await getJSON("/api/events");
    events.innerHTML = rows
      .map((event) => {
        const details = event.details ? JSON.stringify(event.details) : "";
        return `<div class="event ${event.level}">
          <time>${event.createdAt}</time>
          <strong>${event.source}</strong>
          <span>${event.message}</span>
          <code>${details}</code>
        </div>`;
      })
      .join("");
  } catch (error) {
    events.textContent = error.message;
  }
}

document.querySelector("#refresh").addEventListener("click", refresh);
document.querySelector("#probeForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = document.querySelector("#probeResult");
  output.textContent = "Probing...";
  try {
    const data = await getJSON("/api/camera/probe", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: document.querySelector("#cameraUrl").value }),
    });
    renderJSON(output, data);
    await refresh();
  } catch (error) {
    output.textContent = error.message;
    await refresh();
  }
});

refresh();

