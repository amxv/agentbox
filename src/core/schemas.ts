import { z } from "zod";

export const fileReferenceSchema = z.object({
  download_url: z.string().url(),
  file_id: z.string().min(1),
  mime_type: z.string().optional(),
  file_name: z.string().optional()
});

export const createThreadSchema = z.object({
  title: z.string().min(1).max(200)
});

export const postMessageSchema = z.object({
  thread_id: z.string().min(1),
  body: z.string().default(""),
  file: fileReferenceSchema.optional()
});
