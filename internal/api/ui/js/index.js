import { api, esc, state, wire, meta, clean, showError } from "./common.js";

const form = document.getElementById("create");
const list = document.getElementById("vms");
// The grid is redrawn every second, which would re-enable a Delete button
// that is still waiting for its request.
const deleting = new Set();

function tail(text) {
  return esc(clean(text).trimEnd().split("\n").slice(-8).join("\n"));
}

function render(vms, consoles) {
  if (vms.length === 0) {
    list.className = "";
    list.innerHTML = `
      <div class="empty">
        No VMs running. Boot one above, or from a terminal:<br />
        <code>curl -X POST ${esc(location.origin)}/api/vms -d '{"rootfs":"claude"}'</code>
      </div>`;
    return;
  }
  list.className = "vms";
  list.innerHTML = vms
    .map(
      (vm, i) => `
      <article class="vm ${esc(vm.state)}">
        <div class="vm-head">
          <a href="/vm.html?id=${esc(vm.id)}">${esc(vm.id)}</a>
          ${state(vm)}
          <button class="danger" data-delete="${esc(vm.id)}" ${deleting.has(vm.id) ? "disabled" : ""}>Delete</button>
        </div>
        <p class="wire">${wire(vm)}</p>
        <p class="meta">${meta(vm)}</p>
        <pre class="tail">${tail(consoles[i])}</pre>
      </article>`,
    )
    .join("");
}

async function refresh() {
  try {
    const vms = await api("/api/vms");
    const consoles = await Promise.all(vms.map((vm) => api(`/api/vms/${vm.id}/console`).catch(() => "")));
    render(vms, consoles);
  } catch (err) {
    showError(err);
  }
}

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = form.querySelector("button");
  const data = new FormData(form);
  button.disabled = true;
  showError(null);
  try {
    await api("/api/vms", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        rootfs: data.get("rootfs"),
        vcpus: Number(data.get("vcpus")),
        memory_mib: Number(data.get("memory_mib")),
      }),
    });
    await refresh();
  } catch (err) {
    showError(err);
  } finally {
    button.disabled = false;
  }
});

list.addEventListener("click", async (event) => {
  const id = event.target.dataset.delete;
  if (!id || deleting.has(id)) return;
  deleting.add(id);
  event.target.disabled = true;
  try {
    await api(`/api/vms/${id}`, { method: "DELETE" });
  } catch (err) {
    showError(err);
  } finally {
    deleting.delete(id);
  }
  await refresh();
});

const rootfs = await api("/api/rootfs");
document.getElementById("rootfs").innerHTML = rootfs
  .map((name) => `<option>${esc(name)}</option>`)
  .join("");

refresh();
setInterval(refresh, 1000);
