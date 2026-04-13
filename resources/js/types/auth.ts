export type OrganizationRole = 'admin' | 'operator';

export type CurrentOrganization = {
    id: number;
    name: string;
    slug: string;
    image_url: string;
};

export type User = {
    id: number;
    name: string;
    email: string;
    avatar?: string;
    email_verified_at: string | null;
    two_factor_enabled?: boolean;
    current_role: OrganizationRole | null;
    current_organization: CurrentOrganization | null;
    created_at: string;
    updated_at: string;
    [key: string]: unknown;
};

export type OrganizationMembership = {
    id: number;
    name: string;
    slug: string;
    role: OrganizationRole;
    image_url: string;
};

export type Auth = {
    user: User;
    organizations: OrganizationMembership[];
};

export type TwoFactorSetupData = {
    svg: string;
    url: string;
};

export type TwoFactorSecretKey = {
    secretKey: string;
};
