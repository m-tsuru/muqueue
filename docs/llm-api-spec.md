# muqueue API 仕様書（LLM エージェント向け）

## 1. 概要

`muqueue` は、Misskey などの外部サービスから届いたユーザーからの問い合わせ（メンション等）をキューとして管理し、LLM エージェントが統一された HTTP API で問い合わせの取得と回答の返信を行うためのプロキシ API です。

LLM エージェントは各外部サービスの API 仕様や認証情報を直接意識する必要はありません。`muqueue` から未処理のアクションを取得し、回答テキストを `POST /api/actions/:id/answer` に送信するだけで、元サービス（Misskey 等）への返信投稿まで自動で実行されます。

> **注意**: 外部サービスからの Webhook 受信用エンドポイント（`POST /api/query/*`）はサービス側が呼び出すものであり、LLM エージェントからは呼び出しません。

---

## 2. 接続・認証

- **Base URL**: `http://<host>:8080`（環境に応じて読み替えてください）
- **Content-Type**: `application/json`
- **認証方式**: すべての LLM 向けエンドポイントで **HTTP Bearer 認証** が必須です。

```http
Authorization: Bearer <YOUR_API_TOKEN>
```

トークンが欠落している、またはサーバー側で許可されたトークン（`ALLOWED_API_TOKEN`）と一致しない場合は `401 Unauthorized` が返ります。

---

## 3. 推奨ワークフロー

LLM エージェントは以下の手順でキューを処理してください。

1. **未処理キューの取得**: `GET /api/actions` を呼び出し、`status` が `"queued"` のアクション一覧を取得します。
2. **詳細情報の確認（必要に応じて）**: 送信元サービス固有の情報（例: Misskey の公開範囲 `visibility`、Bot 判定 `isBot`、ローカル限定 `isLocal` 等）を判断材料にしたい場合は、`GET /api/actions/:id` を呼び出して `detail` フィールドを確認します。
3. **応答可否の判断**:
   - `user.is_blocked == true` の場合や、ポリシー上応答すべきでない場合（例: `detail.isBot == true` のボットによるループ防止など）は回答を送信せず、必要に応じて `PATCH /api/actions/:id?status=completed` 等でキューから除外してください。
4. **回答の送信**: 回答テキストを生成し、`POST /api/actions/:id/answer` に `{"text": "<回答本文>"}` を送信します。
   - 送信に成功すると、`muqueue` が元サービスへ返信を投稿し、該当アクションの `status` を自動的に `"completed"` へ更新します。
   - 元サービスへの投稿に失敗した場合は `502 Bad Gateway` が返り、`status` は `"failed"` になります。

---

## 4. エンドポイント仕様

### 4.1 疎通確認（Ping）

- **Endpoint**: `GET /` または `GET /api/`
- **認証**: 必須（Bearer Token）
- **用途**: API サーバーの稼働および認証トークンの疎通確認。

**レスポンス (`200 OK`)**:

```json
{
  "message": "pong"
}
```

---

### 4.2 未処理アクション一覧の取得

- **Endpoint**: `GET /api/actions`
- **認証**: 必須（Bearer Token）
- **用途**: `status` が `"queued"` のアクション一覧を `created_at` の降順（新しい順）で取得します。各要素には `user`・`prompt`・`answer` が含まれます。

**レスポンス (`200 OK`)**:

```json
[
  {
    "id": 1,
    "status": "queued",
    "source": "misskey",
    "user_id": 1,
    "user": {
      "id": 1,
      "source": "misskey",
      "name": "毛蟹",
      "user_id_on_source": "s8i",
      "is_verified": false,
      "is_blocked": false,
      "created_at": "2026-11-21T15:22:21Z",
      "updated_at": "2026-11-21T15:22:21Z"
    },
    "prompt": {
      "action_id": 1,
      "text": "@mugyu むぎゅ"
    },
    "answer": {
      "action_id": 0,
      "source": "",
      "text": "",
      "created_at": "0001-01-01T00:00:00Z",
      "post_success": false
    },
    "created_at": "2026-11-21T15:22:21Z",
    "updated_at": "2026-11-21T15:22:21Z"
  }
]
```

> **補足**: 未処理のアクションが存在しない場合は空配列 `[]` が返ります。また、未回答のアクションでは `answer.action_id` が `0`・`answer.text` が `""` となります。

---

### 4.3 アクション詳細の取得（サービス固有情報を含む）

- **Endpoint**: `GET /api/actions/:id`
- **認証**: 必須（Bearer Token）
- **パスパラメータ**:
  - `id` (`uint`, 必須): アクション ID
- **用途**: 指定したアクションの情報に加え、元サービス固有のメタデータを `detail` フィールドとして取得します。ステータスに関わらず取得できます。

**レスポンス (`200 OK` / `source == "misskey"` の例)**:

```json
{
  "id": 1,
  "status": "queued",
  "source": "misskey",
  "user_id": 1,
  "user": {
    "id": 1,
    "source": "misskey",
    "name": "毛蟹",
    "user_id_on_source": "s8i",
    "is_verified": false,
    "is_blocked": false,
    "created_at": "2026-11-21T15:22:21Z",
    "updated_at": "2026-11-21T15:22:21Z"
  },
  "prompt": {
    "action_id": 1,
    "text": "@mugyu むぎゅ"
  },
  "answer": {
    "action_id": 0,
    "source": "",
    "text": "",
    "created_at": "0001-01-01T00:00:00Z",
    "post_success": false
  },
  "created_at": "2026-11-21T15:22:21Z",
  "updated_at": "2026-11-21T15:22:21Z",
  "detail": {
    "ID": 1,
    "CreatedAt": "2026-11-21T15:22:21Z",
    "UpdatedAt": "2026-11-21T15:22:21Z",
    "DeletedAt": null,
    "actionId": 1,
    "host": "https://misskey.y2e.org",
    "visibility": "followers",
    "fromUserId": 1,
    "fromUser": {
      "id": 1,
      "source": "misskey",
      "name": "毛蟹",
      "user_id_on_source": "s8i",
      "is_verified": false,
      "is_blocked": false,
      "created_at": "2026-11-21T15:22:21Z",
      "updated_at": "2026-11-21T15:22:21Z"
    },
    "replyFor": "atu4f6y3xr",
    "isLocal": true,
    "isBot": false
  }
}
```

**エラーレスポンス**:
- `404 Not Found`: 指定 ID のアクションが存在しない場合

---

### 4.4 回答の送信（元サービスへの返信実行）

- **Endpoint**: `POST /api/actions/:id/answer`
- **認証**: 必須（Bearer Token）
- **パスパラメータ**:
  - `id` (`uint`, 必須): 回答対象のアクション ID
- **リクエストボディ (`application/json`)**:

| フィールド | 型 | 必須 | 説明 |
| --- | --- | --- | --- |
| `text` | `string` | 必須 | 元サービスへ返信する回答本文（空文字不可） |

```json
{
  "text": "むぎゅ返し！"
}
```

**サービス別の送信時の挙動**:
- **`misskey`**:
  - 元のメンションノート（`detail.replyFor`）へのリプライとして、`detail.visibility` および `detail.isLocal` を引き継いで `POST {host}/api/notes/create` に投稿されます。
  - **文字数制約**: Misskey API の仕様上、`text` は **1〜3000 文字** の範囲にしてください。

**レスポンス (`200 OK` / 投稿成功時)**:

アクションの `status` は `"completed"` に更新されます。

```json
{
  "action_id": 1,
  "source": "misskey",
  "text": "むぎゅ返し！",
  "created_at": "2026-11-21T15:23:00Z",
  "post_success": true
}
```

**エラーレスポンス**:
- `400 Bad Request`: JSON 形式不正、または `text` が未指定・空文字
- `404 Not Found`: 指定 ID のアクションが存在しない
- `502 Bad Gateway`: 元サービスへの投稿に失敗した場合（`status` は `"failed"`、`post_success` は `false` で保存されます）

```json
{
  "error": "misskey api error (400 INVALID_PARAM): Invalid param.",
  "answer": {
    "action_id": 1,
    "source": "misskey",
    "text": "...",
    "created_at": "2026-11-21T15:23:00Z",
    "post_success": false
  }
}
```

---

### 4.5 アクションステータスの手動更新

- **Endpoint**: `PATCH /api/actions/:id?status=<status>`
- **認証**: 必須（Bearer Token）
- **パスパラメータ**:
  - `id` (`uint`, 必須): アクション ID
- **クエリパラメータ**:
  - `status` (`string`, 必須): 以下のいずれかを指定
    - `queued`: 未処理（キューに戻す）
    - `completed`: 処理済み（返信不要なアクションを閉じる場合など）
    - `failed`: 失敗
- **用途**: 返信を行わずにアクションをスキップしたい場合や、失敗したアクションを管理したい場合に使用します。通常の回答送信では `POST /api/actions/:id/answer` が自動でステータスを更新するため、本エンドポイントの呼び出しは不要です。

**レスポンス (`200 OK`)**:

更新後の `Action` オブジェクトが返ります。

**エラーレスポンス**:
- `400 Bad Request`: `status` クエリパラメータが未指定
- `404 Not Found`: 指定 ID のアクションが存在しない

---

## 5. データモデル定義

### `Action`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `id` | `uint` | アクションの一意 ID |
| `status` | `string` | `"queued"` \| `"completed"` \| `"failed"` |
| `source` | `string` | 発生元サービス名（例: `"misskey"`） |
| `user_id` | `uint` | 送信元ユーザーの内部 ID |
| `user` | `User` | 送信元ユーザー情報 |
| `prompt` | `Prompt` | ユーザーからの質問・メンション本文 |
| `answer` | `PromptAnswer` | LLM による回答履歴 |
| `created_at` | `string` (RFC3339) | 作成日時 |
| `updated_at` | `string` (RFC3339) | 更新日時 |

### `User`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `id` | `uint` | ユーザー内部 ID |
| `source` | `string` | サービス名（例: `"misskey"`） |
| `name` | `string` | 表示名（例: `"毛蟹"`） |
| `user_id_on_source` | `string` | サービス上のユーザー識別子（ローカルは `"s8i"`、リモートは `"s8i@example.com"`） |
| `is_verified` | `boolean` | 認証済みユーザーフラグ |
| `is_blocked` | `boolean` | ブロック対象ユーザーフラグ |

### `Prompt`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `action_id` | `uint` | 紐づく `Action.id` |
| `text` | `string` | ユーザーからの入力テキスト |

### `PromptAnswer`

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `action_id` | `uint` | 紐づく `Action.id` |
| `source` | `string` | 送信先サービス名 |
| `text` | `string` | 送信した回答テキスト |
| `created_at` | `string` (RFC3339) | 回答作成日時 |
| `post_success` | `boolean` | 元サービスへの投稿が成功したか |

### `detail`（`source == "misskey"` の場合: `MisskeyAction`）

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `actionId` | `uint` | 紐づく `Action.id` |
| `host` | `string` | Misskey サーバーのベース URL（例: `"https://misskey.y2e.org"`） |
| `visibility` | `string` | 公開範囲（`"public"` \| `"home"` \| `"followers"` \| `"specified"`） |
| `fromUserId` | `uint` | 送信元 `User.id` |
| `fromUser` | `User` | 送信元 `User` オブジェクト |
| `replyFor` | `string` | 返信先となる元ノートの ID |
| `isLocal` | `boolean` | ローカル限定投稿かどうか |
| `isBot` | `boolean` | 送信元アカウントが Bot かどうか |

---

## 6. エラーハンドリング指針（LLM エージェント向け）

| HTTP ステータス | 原因 | 推奨アクション |
| --- | --- | --- |
| `200 OK` | 成功 | 次のアクションへ進む |
| `400 Bad Request` | リクエスト形式や必須項目の不備 | リクエストボディ（特に `text` が空でないか）を修正して再送 |
| `401 Unauthorized` | Bearer トークン不正・未設定 | 設定を確認し、処理を停止して管理者へ報告 |
| `404 Not Found` | 該当 `id` が存在しない | `GET /api/actions` で最新一覧を再取得 |
| `500 Internal Server Error` | サーバー・DB エラーまたは設定不備 | 時間を置いてリトライ、または管理者へ報告 |
| `502 Bad Gateway` | 元サービス（Misskey 等）への投稿失敗 | `error` の内容を確認（文字数超過・レート制限・元ノート削除等）。文面起因なら修正して再送可能、一時エラーなら待機後リトライ |

---

## 7. リクエスト例（curl）

```bash
export MUQUEUE_URL="http://localhost:8080"
export MUQUEUE_TOKEN="your_api_token_here"

# 1. 未処理アクション一覧を取得
curl -sS -H "Authorization: Bearer ${MUQUEUE_TOKEN}" \
  "${MUQUEUE_URL}/api/actions"

# 2. 特定アクションの詳細（Misskey固有情報含む）を取得
curl -sS -H "Authorization: Bearer ${MUQUEUE_TOKEN}" \
  "${MUQUEUE_URL}/api/actions/1"

# 3. 回答を送信（元サービスへ自動返信 & status=completed）
curl -sS -X POST \
  -H "Authorization: Bearer ${MUQUEUE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"text":"むぎゅ返し！"}' \
  "${MUQUEUE_URL}/api/actions/1/answer"

# 4. 回答せずステータスのみ手動更新する場合
curl -sS -X PATCH \
  -H "Authorization: Bearer ${MUQUEUE_TOKEN}" \
  "${MUQUEUE_URL}/api/actions/1?status=completed"
```

## 8. 補遺

Misskey に関しては、isVerified が true の時だけ私が認証している人間です。

スケジュールの書き込みや照会、メールの送信など、クリティカルな内容は、User の isVerified が true の時だけ実施してください。
