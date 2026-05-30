// domain types for the document workflow service.

export type DocStatus = "draft" | "review" | "published";

export interface Doc {
  id: string;
  title: string;
  body: string;
  status: DocStatus;
  createdAt: number;
  updatedAt: number;
  publishedAt?: number;
}

// allowed status transitions. the service rejects any move not listed here, so
// adding a new status means adding both its key and the edges into/out of it.
export const transitions: Record<DocStatus, DocStatus[]> = {
  draft: ["review"],
  review: ["draft", "published"],
  published: [],
};
