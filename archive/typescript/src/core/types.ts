export type Actor = {
  name: string;
  keyName: string;
};

export type Thread = {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
  created_by: string;
};

export type Asset = {
  id: string;
  message_id: string;
  storage_key: string;
  file_name: string;
  mime_type: string | null;
  size_bytes: number;
  public_url: string | null;
  created_at: string;
  created_by: string;
};

export type Message = {
  id: string;
  thread_id: string;
  author: string;
  body: string;
  created_at: string;
  assets: Asset[];
};

export type ThreadWithMessages = Thread & {
  messages: Message[];
};

export type ChatGPTFileReference = {
  download_url: string;
  file_id: string;
  mime_type?: string;
  file_name?: string;
};
