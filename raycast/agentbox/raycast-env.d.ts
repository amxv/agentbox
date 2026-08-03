/// <reference types="@raycast/api">

/* 🚧 🚧 🚧
 * This file is auto-generated from the extension's manifest.
 * Do not modify manually. Instead, update the `package.json` file.
 * 🚧 🚧 🚧 */

/* eslint-disable @typescript-eslint/ban-types */

type ExtensionPreferences = {
  /** Agentbox URL - Dashboard or API proxy URL. The production dashboard proxies /api requests. */
  "baseUrl": string,
  /** Agentbox API Key - Dedicated Raycast API key for threads, messages, attachments, and visibility. */
  "apiKey": string,
  /** Attachment Download Folder - Folder where attachment download actions save files. */
  "downloadDirectory"?: string
}

/** Preferences accessible in all the extension's commands */
declare type Preferences = ExtensionPreferences

declare namespace Preferences {
  /** Preferences accessible in the `list-threads` command */
  export type ListThreads = ExtensionPreferences & {}
  /** Preferences accessible in the `create-thread` command */
  export type CreateThread = ExtensionPreferences & {}
  /** Preferences accessible in the `post-message` command */
  export type PostMessage = ExtensionPreferences & {}
  /** Preferences accessible in the `doctor` command */
  export type Doctor = ExtensionPreferences & {}
}

declare namespace Arguments {
  /** Arguments passed to the `list-threads` command */
  export type ListThreads = {}
  /** Arguments passed to the `create-thread` command */
  export type CreateThread = {}
  /** Arguments passed to the `post-message` command */
  export type PostMessage = {}
  /** Arguments passed to the `doctor` command */
  export type Doctor = {}
}

