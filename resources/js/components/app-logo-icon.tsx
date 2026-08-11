import type { ImgHTMLAttributes } from 'react';

export default function AppLogoIcon(
    props: ImgHTMLAttributes<HTMLImageElement>,
) {
    const { className, ...imageProps } = props;

    return (
        <>
            <img
                src="/full_logo.png"
                alt="Logo"
                {...imageProps}
                className={
                    className ? `dark:hidden ${className}` : 'dark:hidden'
                }
            />
            <img
                src="/full_logo_white.png"
                alt="Logo"
                {...imageProps}
                className={
                    className
                        ? `hidden dark:block ${className}`
                        : 'hidden dark:block'
                }
            />
        </>
    );
}
