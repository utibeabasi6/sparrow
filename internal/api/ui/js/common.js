export async function api(path, options = {}) {
  const res = await fetch(path, options);
  if (res.status === 204) return null;
  const type = res.headers.get("Content-Type") || "";
  const body = type.includes("application/json") ? await res.json() : await res.text();
  if (!res.ok) throw new Error(body.error || body || res.statusText);
  return body;
}

export function esc(value) {
  const div = document.createElement("div");
  div.textContent = String(value);
  return div.innerHTML;
}

export function state(vm) {
  return `<span class="state ${esc(vm.state)}">${esc(vm.state)}</span>`;
}

export function wire(vm) {
  return `eth0 <b>${esc(vm.guest_ip)}</b><span class="link">⇄</span><b>${esc(vm.tap)}</b>`;
}

export function meta(vm) {
  return [vm.rootfs, `${vm.vcpus} vCPU`, `${vm.memory_mib} MiB`, vm.boot_ms && `booted in ${vm.boot_ms} ms`]
    .filter(Boolean)
    .map(esc)
    .join(" · ");
}

// getty and openrc write terminal control sequences that a <pre> would
// print as junk.
export function clean(text) {
  return text.replace(/\x1b\[[0-9;?]*[A-Za-z]/g, "").replace(/\r/g, "");
}

export function showError(err) {
  const el = document.getElementById("error");
  el.hidden = !err;
  el.textContent = err ? err.message : "";
}
