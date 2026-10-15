import { api, state, wire, meta, clean, showError } from "./common.js";

const id = new URLSearchParams(location.search).get("id");
const consoleEl = document.getElementById("console");

function render(vm) {
  document.title = `${vm.id} · sparrow`;
  document.getElementById("name").textContent = vm.id;
  document.getElementById("state").innerHTML = state(vm);
  document.getElementById("wire").innerHTML = wire(vm);
  document.getElementById("meta").innerHTML = meta(vm);
  document.getElementById("ssh").textContent = `ssh root@${vm.guest_ip}`;
}

async function refresh() {
  try {
    render(await api(`/api/vms/${id}`));
    const text = clean(await api(`/api/vms/${id}/console`));
    const atBottom = consoleEl.scrollTop + consoleEl.clientHeight >= consoleEl.scrollHeight - 20;
    if (consoleEl.textContent !== text) {
      consoleEl.textContent = text;
      if (atBottom) consoleEl.scrollTop = consoleEl.scrollHeight;
    }
    showError(null);
  } catch (err) {
    showError(err);
  }
}

document.getElementById("delete").addEventListener("click", async (event) => {
  event.target.disabled = true;
  try {
    await api(`/api/vms/${id}`, { method: "DELETE" });
    location.href = "/";
  } catch (err) {
    showError(err);
    event.target.disabled = false;
  }
});

refresh();
setInterval(refresh, 1000);
