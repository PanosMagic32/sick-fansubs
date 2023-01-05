import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import { Document } from 'mongoose';

// import { BlogPost } from '@sick/api/blog-post';

export type UserDocument = User & Document;

@Schema()
export class User {
  @Prop({ required: true, unique: true })
  username!: string;

  @Prop({ required: true })
  password!: string;

  @Prop({ required: true, unique: true })
  email!: string;

  @Prop()
  avatar!: string;

  @Prop({ default: false })
  isAdmin!: boolean;

  // @Prop({ type: mongoose.Schema.Types.ObjectId, ref: 'BlogPost' })
  // blogPosts: BlogPost[];
}

export const UserSchema = SchemaFactory.createForClass(User);
