type HeadingVariant = 'default' | 'small' | 'page';

const titleClasses: Record<HeadingVariant, string> = {
    default: 'text-xl font-semibold tracking-tight',
    small: 'mb-0.5 text-base font-medium',
    page: 'text-2xl font-semibold',
};

const headerClasses: Record<HeadingVariant, string> = {
    default: 'mb-8 space-y-0.5',
    small: '',
    page: 'border-b pb-4',
};

export default function Heading({
    title,
    description,
    variant = 'default',
}: {
    title: string;
    description?: string;
    variant?: HeadingVariant;
}) {
    return (
        <header className={headerClasses[variant]}>
            <h2 className={titleClasses[variant]}>{title}</h2>
            {description && (
                <p className="text-sm text-muted-foreground">{description}</p>
            )}
        </header>
    );
}
