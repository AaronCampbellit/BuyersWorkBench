# Workspace and account controls in the sidebar

October 16, 2026. The owner requested moving workspace selection directly beneath the logo and account details/sign-out to the bottom left, removing the top bar so content starts higher.

The authenticated shell keeps one workspace selector and one account control in the sidebar. Workspace switching and sign-out use the existing session/API behavior. Desktop navigation scrolls within the sidebar on short screens while workspace and account controls remain accessible. The main content begins without the former 85-pixel header.

The owner subsequently requested Demo Controls as a single profile-section button. It sits above Sign out on desktop and alongside it, wrapping as needed, on mobile. The six primary navigation entries omit Demo Controls. The button opens the existing demo view, marks its current state and scrolls the content to the top. Existing fictional/reviewer workspace availability is preserved; private workspaces hide it and leave the demo view when selected.

The sidebar was then narrowed to180px on desktop/tablet, with12px horizontal padding and a compact full-name logo. The main content uses the remaining width instead of the former1260px maximum. This returns48px at the standard desktop width and lets large screens use the available table space. The mobile stacked layout and account strip retain their existing behavior.

On narrow screens the logo, workspace and horizontal navigation stay compact at the top. Account controls occupy a bottom strip. A ResizeObserver measures its actual height, including safe-area spacing and any sign-out error; content padding and keyboard scroll clearance reserve that space. Native dialogs remain above the strip. Sign-out failures appear beside the account control and can be retried.

This is a frontend layout change. It does not change database schema, credentials, workspace permissions or purchasing behavior. Browser/build evidence is recorded in [BUILD_STATUS.md](../BUILD_STATUS.md).
