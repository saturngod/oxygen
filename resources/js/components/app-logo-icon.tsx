import type { SVGAttributes } from 'react';

export default function AppLogoIcon(props: SVGAttributes<SVGElement>) {
    return <img src="/full_logo.png" alt="Logo" {...props} />;
}
