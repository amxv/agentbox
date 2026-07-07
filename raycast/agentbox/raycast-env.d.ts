/// <reference types="@raycast/api">

/* 🚧 🚧 🚧
 * This file is auto-generated from the extension's manifest.
 * Do not modify manually. Instead, update the `package.json` file.
 * 🚧 🚧 🚧 */

/* eslint-disable @typescript-eslint/ban-types */

type ExtensionPreferences = {
  /** Agentbox URL - Dashboard or API proxy URL. The production dashboard proxies /api requests. */
  "baseUrl": string,
  /** Agentbox API Key - Actor API key for thread, message, attachment, and MCP requests. */
  "apiKey": string,
  /** Attachment Download Folder - Folder where attachment download actions save files. */
  "downloadDirectory"?: string
}

/** Preferences accessible in all the extension's commands */
declare type Preferences = ExtensionPreferences

declare namespace Preferences {
  /** Preferences accessible in the `latest-messages` command */
  export type LatestMessages = ExtensionPreferences & {}
  /** Preferences accessible in the `search-threads` command */
  export type SearchThreads = ExtensionPreferences & {}
  /** Preferences accessible in the `list-threads` command */
  export type ListThreads = ExtensionPreferences & {}
  /** Preferences accessible in the `post-message` command */
  export type PostMessage = ExtensionPreferences & {}
  /** Preferences accessible in the `doctor` command */
  export type Doctor = ExtensionPreferences & {}
}

declare namespace Arguments {
  /** Arguments passed to the `latest-messages` command */
  export type LatestMessages = {}
  /** Arguments passed to the `search-threads` command */
  export type SearchThreads = {}
  /** Arguments passed to the `list-threads` command */
  export type ListThreads = {}
  /** Arguments passed to the `post-message` command */
  export type PostMessage = {}
  /** Arguments passed to the `doctor` command */
  export type Doctor = {}
}

