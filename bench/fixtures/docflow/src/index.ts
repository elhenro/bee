// wiring: build a service backed by a fresh repo + bus. tests and callers use
// this factory so everyone shares the same construction.

import { DocRepository } from "./repository.ts";
import { EventBus } from "./events.ts";
import { DocService } from "./service.ts";

export function createService(): { service: DocService; bus: EventBus; repo: DocRepository } {
  const repo = new DocRepository();
  const bus = new EventBus();
  const service = new DocService(repo, bus);
  return { service, bus, repo };
}

export { DocService } from "./service.ts";
export { DocRepository } from "./repository.ts";
export { EventBus } from "./events.ts";
export * from "./types.ts";
export * from "./errors.ts";
