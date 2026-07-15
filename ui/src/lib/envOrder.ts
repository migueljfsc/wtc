// There is no server-side env ordering, so the portal applies a promotion-order
// heuristic for matrix columns: known stage names first (dev → … → prod), the
// rest alphabetical. Ephemeral pr-* envs are excluded from the matrix by default.
const PREF = [
  "dev", "development", "test", "qa", "int", "integration",
  "stage", "staging", "preprod", "pre-prod", "uat", "canary",
  "prod", "production",
];

export function orderEnvs(envs: string[]): string[] {
  return [...envs].sort((a, b) => {
    const ia = PREF.indexOf(a.toLowerCase());
    const ib = PREF.indexOf(b.toLowerCase());
    if (ia !== -1 && ib !== -1) return ia - ib;
    if (ia !== -1) return -1;
    if (ib !== -1) return 1;
    return a.localeCompare(b);
  });
}

export function isEphemeral(env: string): boolean {
  return /^pr-/.test(env);
}
