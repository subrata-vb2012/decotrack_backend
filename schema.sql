-- DecoTrack PostgreSQL Database Schema

-- Enable UUID extension if not enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create Custom ENUM Types
DO $$ 
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'member_role') THEN
        CREATE TYPE member_role AS ENUM ('owner', 'admin', 'secretary', 'member');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'member_status') THEN
        CREATE TYPE member_status AS ENUM ('active', 'pending', 'blocked');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'inventory_type') THEN
        CREATE TYPE inventory_type AS ENUM ('lent', 'catering');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'lending_status') THEN
        CREATE TYPE lending_status AS ENUM ('active', 'partiallyReturned', 'closed');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'account_entry_type') THEN
        CREATE TYPE account_entry_type AS ENUM ('openingBalance', 'credit', 'debit');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'account_entry_status') THEN
        CREATE TYPE account_entry_status AS ENUM ('completed', 'pending');
    END IF;
END $$;

-- 1. Users Table
CREATE TABLE IF NOT EXISTS users (
    id VARCHAR(128) PRIMARY KEY, -- Matches Google Subject ID / Sub
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    photo_url TEXT,
    mobile_number VARCHAR(20),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. Clubs Table
CREATE TABLE IF NOT EXISTS clubs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    registration_number VARCHAR(100),
    email VARCHAR(255) NOT NULL,
    photo_url TEXT,
    invite_code VARCHAR(10) UNIQUE NOT NULL,
    owner_id VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. Club Members Table (Pivot Table)
CREATE TABLE IF NOT EXISTS club_members (
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    role member_role NOT NULL DEFAULT 'member',
    status member_status NOT NULL DEFAULT 'pending',
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (club_id, user_id)
);

-- 4. Device FCM Tokens Table (Linked to User for Push Notifications)
CREATE TABLE IF NOT EXISTS user_fcm_tokens (
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    token TEXT PRIMARY KEY,
    platform VARCHAR(50) DEFAULT 'mobile',
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 5. Inventory Table
CREATE TABLE IF NOT EXISTS inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    total_qty INT NOT NULL CHECK (total_qty >= 0),
    available_qty INT NOT NULL CHECK (available_qty >= 0),
    category VARCHAR(100) NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    type inventory_type NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_available_qty CHECK (available_qty <= total_qty)
);

-- 6. Lendings Table
CREATE TABLE IF NOT EXISTS lendings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    customer_name VARCHAR(255) NOT NULL,
    customer_mobile VARCHAR(20),
    customer_address TEXT,
    purpose TEXT,
    expected_return_date TIMESTAMP WITH TIME ZONE,
    amount NUMERIC(12, 2) CHECK (amount >= 0),
    status lending_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL
);

-- 7. Lending Items Table
CREATE TABLE IF NOT EXISTS lending_items (
    lending_id UUID REFERENCES lendings(id) ON DELETE CASCADE,
    inventory_id UUID REFERENCES inventory(id) ON DELETE CASCADE,
    quantity INT NOT NULL CHECK (quantity > 0),
    returned_quantity INT NOT NULL DEFAULT 0 CHECK (returned_quantity >= 0),
    PRIMARY KEY (lending_id, inventory_id),
    CONSTRAINT chk_returned_quantity CHECK (returned_quantity <= quantity)
);

-- 8. Lending Returns Table
CREATE TABLE IF NOT EXISTS lending_returns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lending_id UUID REFERENCES lendings(id) ON DELETE CASCADE,
    note TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL
);

-- 9. Lending Return Items Table
CREATE TABLE IF NOT EXISTS lending_return_items (
    return_id UUID REFERENCES lending_returns(id) ON DELETE CASCADE,
    inventory_id UUID REFERENCES inventory(id) ON DELETE CASCADE,
    quantity INT NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (return_id, inventory_id)
);

-- 10. Notices Table (Announcements, RSVP, Polls)
CREATE TABLE IF NOT EXISTS notices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    is_event BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL
);

-- 11. Notice Poll Options Table
CREATE TABLE IF NOT EXISTS notice_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notice_id UUID REFERENCES notices(id) ON DELETE CASCADE,
    option_text VARCHAR(100) NOT NULL,
    UNIQUE (notice_id, option_text)
);

-- 12. Notice Poll Votes Table
CREATE TABLE IF NOT EXISTS notice_votes (
    notice_id UUID REFERENCES notices(id) ON DELETE CASCADE,
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    option_id UUID REFERENCES notice_options(id) ON DELETE CASCADE,
    PRIMARY KEY (notice_id, user_id)
);

-- 13. Club Account Entries Table (Financial Ledger)
CREATE TABLE IF NOT EXISTS club_account_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    purpose VARCHAR(255) NOT NULL,
    type account_entry_type NOT NULL,
    status account_entry_status NOT NULL DEFAULT 'completed',
    amount NUMERIC(12, 2) NOT NULL CHECK (amount >= 0),
    entry_date TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMP WITH TIME ZONE,
    updated_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL
);
