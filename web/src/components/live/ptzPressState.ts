export type PtzPressSource = "pointer" | "keyboard";

export function updatePtzPressSources(
  current: ReadonlySet<PtzPressSource>,
  source: PtzPressSource,
  active: boolean,
): ReadonlySet<PtzPressSource> {
  const next = new Set(current);
  if (active) next.add(source);
  else next.delete(source);
  return next;
}
