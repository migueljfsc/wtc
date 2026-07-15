/** Runtime configuration injected by /config.js (window.__WTC_CONFIG__). */
export interface RuntimeConfig {
  /** Base URL of the wtc API. Empty string => same origin (proxy setups). */
  apiBaseUrl: string;
}

declare global {
  interface Window {
    __WTC_CONFIG__?: Partial<RuntimeConfig>;
  }
}

export const config: RuntimeConfig = {
  apiBaseUrl: window.__WTC_CONFIG__?.apiBaseUrl ?? "",
};
