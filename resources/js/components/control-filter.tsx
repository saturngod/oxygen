import type { ReactNode } from 'react';
import { Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

type ActionButton = {
    label: string;
    icon?: ReactNode;
    onClick?: () => void;
    variant?:
        | 'default'
        | 'destructive'
        | 'outline'
        | 'secondary'
        | 'ghost'
        | 'link';
};

export function ControlFilter({
    searchValue,
    onSearchChange,
    searchPlaceholder = 'Search...',
    actions = [],
}: {
    searchValue: string;
    onSearchChange: (value: string) => void;
    searchPlaceholder?: string;
    actions?: ActionButton[];
}) {
    return (
        <div className="flex items-center gap-2">
            <div className="relative flex-1">
                <Search className="absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                    type="search"
                    value={searchValue}
                    onChange={(e) => onSearchChange(e.target.value)}
                    placeholder={searchPlaceholder}
                    className="pl-7"
                />
            </div>
            {actions.map((action) => (
                <Button
                    key={action.label}
                    variant={action.variant ?? 'default'}
                    onClick={action.onClick}
                >
                    {action.icon}
                    {action.label}
                </Button>
            ))}
        </div>
    );
}
