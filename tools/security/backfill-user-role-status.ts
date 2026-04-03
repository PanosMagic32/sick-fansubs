import mongoose from 'mongoose';

type UserRole = 'super-admin' | 'admin' | 'moderator' | 'user';
type UserStatus = 'active' | 'suspended';

interface DbUser {
  _id: mongoose.Types.ObjectId;
  email?: string;
  isAdmin?: boolean;
  role?: UserRole;
  status?: UserStatus;
}

function getArg(name: string): string | undefined {
  const prefix = `--${name}=`;
  const arg = process.argv.find((value) => value.startsWith(prefix));
  return arg?.slice(prefix.length).trim() || undefined;
}

async function run() {
  const uri = process.env['DATABASE_URL_DEV'] || process.env['DATABASE_URL'];

  if (!uri) {
    throw new Error('Missing DATABASE_URL_DEV or DATABASE_URL environment variable.');
  }

  const dbName =
    getArg('dbName') ||
    process.env['DATABASE_NAME'] ||
    (process.env['NODE_ENV'] === 'production' ? 'sick-db' : 'dev-sick-db');

  const superAdminEmail = getArg('superAdminEmail') || process.env['SUPER_ADMIN_EMAIL'];

  await mongoose.connect(uri, { dbName });

  const usersCollection = mongoose.connection.collection<DbUser>('users');

  const usersToNormalize = await usersCollection
    .find(
      {
        $or: [{ role: { $exists: false } }, { status: { $exists: false } }],
      },
      {
        projection: { _id: 1, isAdmin: 1, role: 1, status: 1 },
      },
    )
    .toArray();

  if (usersToNormalize.length > 0) {
    const operations = usersToNormalize.map((user) => {
      const role: UserRole = user.role ?? (user.isAdmin ? 'admin' : 'user');
      const status: UserStatus = user.status ?? 'active';
      const isAdmin = role === 'admin' || role === 'super-admin';

      return {
        updateOne: {
          filter: { _id: user._id },
          update: { $set: { role, status, isAdmin } },
        },
      };
    });

    await usersCollection.bulkWrite(operations);
    console.log(`[backfill-user-role-status] Normalized ${usersToNormalize.length} user records.`);
  } else {
    console.log('[backfill-user-role-status] No user normalization needed.');
  }

  if (superAdminEmail) {
    const result = await usersCollection.updateOne(
      { email: superAdminEmail },
      { $set: { role: 'super-admin', status: 'active', isAdmin: true } },
    );

    if (result.matchedCount === 0) {
      throw new Error(`No user found with email ${superAdminEmail} to promote as super-admin.`);
    }

    console.log(`[backfill-user-role-status] Promoted ${superAdminEmail} to super-admin.`);
  } else {
    console.log('[backfill-user-role-status] SUPER_ADMIN_EMAIL not provided; skipped super-admin promotion step.');
  }

  const superAdminCount = await usersCollection.countDocuments({ role: 'super-admin' });
  if (superAdminCount === 0) {
    console.warn('[backfill-user-role-status] Warning: no super-admin users exist after backfill.');
  }

  await mongoose.disconnect();
}

run().catch(async (error: unknown) => {
  console.error('[backfill-user-role-status] Failed:', error);
  await mongoose.disconnect();
  process.exitCode = 1;
});
