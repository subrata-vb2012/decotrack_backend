# 📖 DecoTrack Go Backend: Complete API Endpoints Specification

This document details the complete REST API specification for the **DecoTrack** Go backend. All endpoints (except Google Login) require a secure custom JWT sent in the `Authorization: Bearer <custom_jwt_token>` header.

---

## 🔒 1. Authentication & User Profile

### A. Google Sign-In Login & Registration
* **Endpoint:** `POST /api/v1/auth/google`
* **Auth Required:** No
* **Description:** Verifies the Google ID Token directly with Google servers. If the user is new, creates their profile. Returns a custom, server-signed session JWT.

#### Request Body
```json
{
  "idToken": "eyJhbGciOiJSUzI1NiIsImtpZCI6IjFhOWRiN..."
}
```

#### Response: Success (`200 OK`)
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyVUlEI...",
  "user": {
    "uid": "google_sub_10928374656",
    "name": "Subrata Ghosh",
    "email": "subratavb2012@gmail.com",
    "photoUrl": "https://lh3.googleusercontent.com/a/ALm5wu...",
    "mobileNumber": null,
    "createdAt": "2026-05-30T07:10:00Z"
  }
}
```

#### Response: Error - Invalid Token (`401 Unauthorized`)
```json
{
  "error": "invalid google token: token is expired or signature is invalid"
}
```

---

### B. Fetch Current Profile
* **Endpoint:** `GET /api/v1/users/me`
* **Auth Required:** Yes
* **Description:** Retrieves the profile settings of the currently authenticated user.

#### Response: Success (`200 OK`)
```json
{
  "uid": "google_sub_10928374656",
  "name": "Subrata Ghosh",
  "email": "subratavb2012@gmail.com",
  "photoUrl": "https://lh3.googleusercontent.com/a/ALm5wu...",
  "mobileNumber": "+919876543210",
  "createdAt": "2026-05-30T07:10:00Z"
}
```

---

### C. Update Profile Details
* **Endpoint:** `PUT /api/v1/users/me`
* **Auth Required:** Yes
* **Description:** Updates the profile settings (name and mobile number).

#### Request Body
```json
{
  "name": "Subrata Ghosh",
  "mobileNumber": "+919876543210"
}
```

#### Response: Success (`200 OK`)
```json
{
  "uid": "google_sub_10928374656",
  "name": "Subrata Ghosh",
  "email": "subratavb2012@gmail.com",
  "photoUrl": "https://lh3.googleusercontent.com/a/ALm5wu...",
  "mobileNumber": "+919876543210",
  "createdAt": "2026-05-30T07:10:00Z"
}
```

---

### D. Register/Update Device FCM Token
* **Endpoint:** `POST /api/v1/users/me/fcm-token`
* **Auth Required:** Yes
* **Description:** Links a device's Firebase Cloud Messaging token to the user for push notifications.

#### Request Body
```json
{
  "token": "d_1x8e9ab283cd78eef0...",
  "platform": "android"
}
```

#### Response: Success (`200 OK`)
```json
{
  "status": "registered",
  "message": "FCM token updated successfully"
}
```

---

## 🏛️ 2. Club Management

### A. Create a Club
* **Endpoint:** `POST /api/v1/clubs`
* **Auth Required:** Yes
* **Description:** Registers a new club. The creator is automatically assigned the `owner` role.

#### Request Body
```json
{
  "name": "Apex Decorators",
  "address": "Kolkata, WB, India",
  "registrationNumber": "REG-88192A",
  "email": "apex@example.com",
  "photoUrl": "https://example.com/club.jpg"
}
```

#### Response: Success (`201 Created`)
```json
{
  "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
  "name": "Apex Decorators",
  "address": "Kolkata, WB, India",
  "registrationNumber": "REG-88192A",
  "email": "apex@example.com",
  "photoUrl": "https://example.com/club.jpg",
  "inviteCode": "APEX492",
  "ownerId": "google_sub_10928374656",
  "createdAt": "2026-05-30T07:15:00Z"
}
```

---

### B. List User Clubs
* **Endpoint:** `GET /api/v1/clubs`
* **Auth Required:** Yes
* **Description:** Retrieves all clubs where the user is an active, pending, or blocked member.

#### Response: Success (`200 OK`)
```json
[
  {
    "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
    "name": "Apex Decorators",
    "address": "Kolkata, WB, India",
    "registrationNumber": "REG-88192A",
    "email": "apex@example.com",
    "photoUrl": "https://example.com/club.jpg",
    "inviteCode": "APEX492",
    "ownerId": "google_sub_10928374656",
    "createdAt": "2026-05-30T07:15:00Z",
    "myRole": "owner",
    "myStatus": "active"
  }
]
```

---

### C. Verify Invite Code
* **Endpoint:** `GET /api/v1/clubs/invite/:code`
* **Auth Required:** Yes
* **Description:** Validates a club invite code and returns metadata before joining.

#### Response: Success (`200 OK`)
```json
{
  "id": "a1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d",
  "name": "Apex Decorators",
  "photoUrl": "https://example.com/club.jpg",
  "ownerName": "Subrata Ghosh"
}
```

---

### D. Request to Join Club
* **Endpoint:** `POST /api/v1/clubs/join`
* **Auth Required:** Yes
* **Description:** Requests to join a club using an invite code. Places the member in `pending` status.

#### Request Body
```json
{
  "inviteCode": "APEX492"
}
```

#### Response: Success (`202 Accepted`)
```json
{
  "status": "pending_approval",
  "message": "Your request to join Apex Decorators has been submitted to administrators."
}
```

---

## 👥 3. Member Management

### A. List Club Members
* **Endpoint:** `GET /api/v1/clubs/:clubId/members`
* **Auth Required:** Yes
* **Description:** Retrieves all members of a specific club. Can filter by status (`active`, `pending`, `blocked`).

#### Query Parameters
* `status` (Optional) - Filter by `active`, `pending`, or `blocked`.

#### Response: Success (`200 OK`)
```json
[
  {
    "userId": "google_sub_10928374656",
    "name": "Subrata Ghosh",
    "email": "subratavb2012@gmail.com",
    "mobileNumber": "+919876543210",
    "photoUrl": "https://lh3.googleusercontent.com/a/ALm5wu...",
    "role": "owner",
    "status": "active",
    "joinedAt": "2026-05-30T07:15:00Z"
  }
]
```

---

### B. Update Member Role
* **Endpoint:** `PUT /api/v1/clubs/:clubId/members/:userId/role`
* **Auth Required:** Yes (Requires `owner` or `admin` requester role)
* **Description:** Promotes or demotes a member's role (`admin`, `secretary`, `member`).

#### Request Body
```json
{
  "role": "secretary"
}
```

#### Response: Success (`200 OK`)
```json
{
  "userId": "google_sub_88912389a",
  "role": "secretary",
  "message": "Member role updated successfully"
}
```

---

### C. Update Member Status
* **Endpoint:** `PUT /api/v1/clubs/:clubId/members/:userId/status`
* **Auth Required:** Yes (Requires `owner` or `admin` requester role)
* **Description:** Approves, blocks, or unblocks a member's status (`active`, `blocked`).

#### Request Body
```json
{
  "status": "active"
}
```

#### Response: Success (`200 OK`)
```json
{
  "userId": "google_sub_88912389a",
  "status": "active",
  "message": "Member status updated successfully"
}
```

---

## 📦 4. Inventory Management

### A. Fetch Club Inventory
* **Endpoint:** `GET /api/v1/clubs/:clubId/inventory`
* **Auth Required:** Yes (Requester must be an active club member)
* **Description:** Retrieves all inventory assets, filterable by type (`lent` or `catering`).

#### Query Parameters
* `type` (Optional) - Filter by `lent` or `catering`.

#### Response: Success (`200 OK`)
```json
[
  {
    "id": "e1f2g3h4-i5j6-7k8l-9m0n-1o2p3q4r5s6t",
    "name": "Luxury LED Lights",
    "totalQty": 50,
    "availableQty": 42,
    "category": "Lighting",
    "isActive": true,
    "type": "lent",
    "createdAt": "2026-05-30T07:30:00Z"
  }
]
```

---

### B. Add Inventory Asset
* **Endpoint:** `POST /api/v1/clubs/:clubId/inventory`
* **Auth Required:** Yes (Requires `owner`, `admin`, or `secretary` requester role)
* **Description:** Adds a new asset to the catalog.

#### Request Body
```json
{
  "name": "Buffet Warmer Set",
  "totalQty": 15,
  "category": "Catering",
  "type": "catering"
}
```

#### Response: Success (`201 Created`)
```json
{
  "id": "c1d2e3f4-g5h6-7i8j-9k0l-1m2n3o4p5q6r",
  "name": "Buffet Warmer Set",
  "totalQty": 15,
  "availableQty": 15,
  "category": "Catering",
  "isActive": true,
  "type": "catering",
  "createdAt": "2026-05-30T08:00:00Z"
}
```

---

### C. Update Inventory Asset
* **Endpoint:** `PUT /api/v1/clubs/:clubId/inventory/:itemId`
* **Auth Required:** Yes (Requires `owner`, `admin`, or `secretary` requester role)
* **Description:** Updates details or increases/decreases total quantities of an asset.

#### Request Body
```json
{
  "name": "Buffet Warmer Set (Premium)",
  "totalQty": 20,
  "category": "Catering",
  "isActive": true
}
```

#### Response: Success (`200 OK`)
```json
{
  "id": "c1d2e3f4-g5h6-7i8j-9k0l-1m2n3o4p5q6r",
  "name": "Buffet Warmer Set (Premium)",
  "totalQty": 20,
  "availableQty": 20,
  "category": "Catering",
  "isActive": true,
  "type": "catering",
  "createdAt": "2026-05-30T08:00:00Z"
}
```

---

## 🤝 5. Lending & Returns Workflows

### A. Create Lending Order (ACID Stock Check)
* **Endpoint:** `POST /api/v1/clubs/:clubId/lendings`
* **Auth Required:** Yes
* **Description:** Initiates a lending checkout. Runs inside an atomic database transaction to lock items, verify sufficient `availableQty`, decrement stock, and log the checkout record.

#### Request Body
```json
{
  "customerName": "Rahul Sharma",
  "customerMobile": "+919876543211",
  "customerAddress": "Sector 5, Salt Lake, Kolkata",
  "purpose": "Wedding Stage Setup",
  "expectedReturnDate": "2026-06-05T18:00:00Z",
  "amount": 12000.50,
  "items": [
    {
      "itemId": "e1f2g3h4-i5j6-7k8l-9m0n-1o2p3q4r5s6t",
      "name": "Luxury LED Lights",
      "quantity": 8
    }
  ]
}
```

#### Response: Success (`201 Created`)
```json
{
  "lendingId": "l1m2n3o4-p5q6-7r8s-9t0u-1v2w3x4y5z6a",
  "status": "active",
  "message": "Lending record created and inventory quantities successfully updated."
}
```

#### Response: Error - Insufficient Stock (`400 Bad Request`)
```json
{
  "error": "Insufficient stock for: Luxury LED Lights (Requested: 8, Available: 5)"
}
```

---

### B. Record Return (Full or Partial)
* **Endpoint:** `POST /api/v1/clubs/:clubId/lendings/:lendingId/returns`
* **Auth Required:** Yes
* **Description:** Records returning full or partial stock of an active lending order. Increments `availableQty` inside inventory, and updates the lending status to `partiallyReturned` or `closed` automatically.

#### Request Body
```json
{
  "items": [
    {
      "itemId": "e1f2g3h4-i5j6-7k8l-9m0n-1o2p3q4r5s6t",
      "quantity": 8
    }
  ],
  "note": "All lights returned in good condition."
}
```

#### Response: Success (`200 OK`)
```json
{
  "returnId": "r1s2t3u4-v5w6-7x8y-9z0a-1b2c3d4e5f6g",
  "lendingStatus": "closed",
  "message": "Return recorded and inventory stocks restored successfully."
}
```

---

## 📢 6. Notice Board & Announcements

### A. Create a Notice/Event/Poll
* **Endpoint:** `POST /api/v1/clubs/:clubId/notices`
* **Auth Required:** Yes
* **Description:** Posts an announcement, RSVP event, or voting poll. Automatically queries member FCM tokens in the background and sends a push notification to their devices!

#### Request Body (For a Voting Poll)
```json
{
  "title": "Purchase Catering warmers?",
  "message": "Should we purchase 5 extra warmers for upcoming summer events?",
  "isEvent": false,
  "options": ["yes", "no"]
}
```

#### Response: Success (`201 Created`)
```json
{
  "id": "n1o2p3q4-r5s6-7t8u-9v0w-1x2y3z4a5b6c",
  "title": "Purchase Catering warmers?",
  "message": "Should we purchase 5 extra warmers?",
  "isEvent": false,
  "votes": {
    "yes": 0,
    "no": 0
  },
  "createdAt": "2026-05-30T08:30:00Z"
}
```

---

### B. Cast a Vote
* **Endpoint:** `POST /api/v1/clubs/:clubId/notices/:noticeId/vote`
* **Auth Required:** Yes
* **Description:** Casts or changes a user's vote on an active notice board poll.

#### Request Body
```json
{
  "option": "yes"
}
```

#### Response: Success (`200 OK`)
```json
{
  "noticeId": "n1o2p3q4-r5s6-7t8u-9v0w-1x2y3z4a5b6c",
  "votes": {
    "yes": 12,
    "no": 3
  },
  "message": "Vote cast successfully"
}
```

---

## 💰 7. Financial Ledger & Accounts

### A. List Transaction Entries (Ledger)
* **Endpoint:** `GET /api/v1/clubs/:clubId/accounts`
* **Auth Required:** Yes
* **Description:** Retrieves all transaction entries for a club, sorted chronologically. Can be filtered by transaction status (e.g., current/completed or pending).

#### Query Parameters
* `status` (Optional) - Filter by `completed` or `pending` to isolate current vs. pending transactions.

#### Response: Success (`200 OK`)
```json
[
  {
    "id": "a1c2e3g4-h5i6-7j8k-9l0m-1n2o3p4q5r6s",
    "purpose": "Purchased extra fabrics",
    "type": "debit",
    "status": "completed",
    "amount": 4500.00,
    "entryDate": "2026-05-30T09:00:00Z",
    "createdAt": "2026-05-30T09:02:00Z",
    "createdBy": "google_sub_10928374656",
    "updatedAt": null,
    "updatedBy": null
  },
  {
    "id": "b2d4f6h8-i0j2-4k6l-8m0n-2o4p6q8r0s2t",
    "purpose": "Member rent subscription deposit",
    "type": "credit",
    "status": "pending",
    "amount": 2500.00,
    "entryDate": "2026-05-30T10:00:00Z",
    "createdAt": "2026-05-30T10:05:00Z",
    "createdBy": "google_sub_10928374656",
    "updatedAt": null,
    "updatedBy": null
  }
]
```

---

### B. Record a Transaction Entry
* **Endpoint:** `POST /api/v1/clubs/:clubId/accounts`
* **Auth Required:** Yes (Requires `owner`, `admin`, or `secretary` requester role)
* **Description:** Records an expense (`debit`), income (`credit`), or `openingBalance` entry, setting the transaction status as `completed` or `pending`.

#### Request Body
```json
{
  "purpose": "Purchased extra fabrics",
  "type": "debit",
  "status": "completed",
  "amount": 4500.00,
  "entryDate": "2026-05-30T09:00:00Z"
}
```

#### Response: Success (`201 Created`)
```json
{
  "id": "a1c2e3g4-h5i6-7j8k-9l0m-1n2o3p4q5r6s",
  "purpose": "Purchased extra fabrics",
  "type": "debit",
  "status": "completed",
  "amount": 4500.00,
  "entryDate": "2026-05-30T09:00:00Z",
  "createdAt": "2026-05-30T09:02:00Z",
  "createdBy": "google_sub_10928374656"
}
```

---

### C. Update a Transaction Entry
* **Endpoint:** `PUT /api/v1/clubs/:clubId/accounts/:entryId`
* **Auth Required:** Yes (Requires `owner` or `admin` requester role)
* **Description:** Updates the purpose, type, status, amount, or date of an existing transaction record. Automatically updates audit fields.

#### Request Body
```json
{
  "purpose": "Purchased extra fabrics (Refurbished)",
  "type": "debit",
  "status": "completed",
  "amount": 4200.00,
  "entryDate": "2026-05-30T09:00:00Z"
}
```

#### Response: Success (`200 OK`)
```json
{
  "id": "a1c2e3g4-h5i6-7j8k-9l0m-1n2o3p4q5r6s",
  "purpose": "Purchased extra fabrics (Refurbished)",
  "type": "debit",
  "status": "completed",
  "amount": 4200.00,
  "entryDate": "2026-05-30T09:00:00Z",
  "createdAt": "2026-05-30T09:02:00Z",
  "createdBy": "google_sub_10928374656",
  "updatedAt": "2026-05-30T11:00:00Z",
  "updatedBy": "google_sub_10928374656"
}
```

---

### D. Approve / Update Pending Transaction Status
* **Endpoint:** `PATCH /api/v1/clubs/:clubId/accounts/:entryId/status`
* **Auth Required:** Yes (Requires `owner`, `admin`, or `secretary` requester role)
* **Description:** Approves or completes a pending transaction (e.g. promoting status from `pending` to `completed` once money changes hands).

#### Request Body
```json
{
  "status": "completed"
}
```

#### Response: Success (`200 OK`)
```json
{
  "id": "b2d4f6h8-i0j2-4k6l-8m0n-2o4p6q8r0s2t",
  "status": "completed",
  "message": "Transaction status updated successfully and included in active balances."
}
```

---

### E. Fetch Accounts Summary (Running Balance)
* **Endpoint:** `GET /api/v1/clubs/:clubId/accounts/balance`
* **Auth Required:** Yes
* **Description:** Computes the total completed credits, total completed debits, and the current running net balance. Pending transactions are excluded from active calculations but logged separately.

#### Response: Success (`200 OK`)
```json
{
  "runningBalance": 18200.75,
  "totalCredit": 32000.00,
  "totalDebit": 13799.25,
  "totalPendingCredit": 2500.00,
  "totalPendingDebit": 0.00
}
```
