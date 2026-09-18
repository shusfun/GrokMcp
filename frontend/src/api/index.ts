import type { Client } from "./client";
import { mockClient } from "./mock";
import { isWails, wailsClient } from "./wails";

export type { Client } from "./client";

export function getClient(): Client {
  return isWails() ? wailsClient : mockClient;
}
