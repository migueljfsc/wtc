// The bearer token is a value from the server's auth.api_tokens. v1 has no
// user login — the token IS the credential — so it lives in localStorage and
// rides every API request as a bearer header.
const TOKEN_KEY = "wtc.token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}
