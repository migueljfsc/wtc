import createClient from "openapi-fetch";
import type { paths } from "@/api/schema";
import { config } from "@/lib/config";
import { getToken } from "@/lib/token";

// One typed client for the whole app. baseUrl comes from runtime config so the
// same build talks to any wtc server; the bearer token is injected per-request
// from localStorage via middleware.
export const api = createClient<paths>({ baseUrl: config.apiBaseUrl });

api.use({
  onRequest({ request }) {
    const token = getToken();
    if (token) {
      request.headers.set("Authorization", `Bearer ${token}`);
    }
    return request;
  },
});

/**
 * verifyToken calls the auth-check endpoint with a candidate token (not the
 * stored one) and resolves true when the server accepts it. Used by the login
 * screen before persisting the token.
 */
export async function verifyToken(token: string): Promise<boolean> {
  const res = await fetch(`${config.apiBaseUrl}/api/v1/auth/verify`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  return res.ok;
}
