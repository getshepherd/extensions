// The page's side of paddock: call(name, args) runs a command, on(event, fn) hears what Ctx.Emit sends.

export async function call(name, args = {}) {
  const res = await fetch('/api/' + encodeURIComponent(name), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(args),
  });
  const body = await res.json();
  if (!res.ok) throw new Error(body.error);
  return body;
}

let source;

export function on(event, fn) {
  source ??= new EventSource('/_paddock/events');
  source.addEventListener(event, (e) => fn(JSON.parse(e.data)));
}
